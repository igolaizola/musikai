package s3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// New returns a new S3 image store.
func New(key, secret, region, bucket string, debug bool) (*Store, error) {
	httpClient := &http.Client{
		Timeout: 60 * time.Second,
	}
	s := &Store{
		key:        key,
		secret:     secret,
		region:     region,
		bucket:     bucket,
		debug:      debug,
		httpClient: httpClient,
	}
	if err := s.start(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

type Store struct {
	key        string
	secret     string
	region     string
	bucket     string
	debug      bool
	client     *s3.Client
	httpClient *http.Client
}

func (s *Store) PublicURL(name string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, name)
}

func (s *Store) start(ctx context.Context) error {
	var cfg aws.Config
	if s.region == "tebi" {
		customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				PartitionID:   "aws",
				URL:           "https://s3.tebi.io",
				SigningRegion: "de",
			}, nil
		})

		customConfig := config.LoadOptionsFunc(func(configOptions *config.LoadOptions) error {
			configOptions.Credentials = credentials.StaticCredentialsProvider{Value: aws.Credentials{AccessKeyID: s.key, SecretAccessKey: s.secret}}
			return nil
		})
		candidate, err := config.LoadDefaultConfig(ctx, config.WithEndpointResolverWithOptions(customResolver), customConfig)
		if err != nil {
			return fmt.Errorf("s3: couldn't load tebi config: %w", err)
		}
		cfg = candidate
	} else {
		var provider aws.CredentialsProvider
		if s.key == "" && s.secret == "" {
			// Load credentials from EC2 Instance Role
			provider = ec2rolecreds.New()
		} else {
			// Load credentials from static credentials
			provider = credentials.NewStaticCredentialsProvider(s.key, s.secret, "")
		}
		candidate, err := config.LoadDefaultConfig(ctx,
			config.WithCredentialsProvider(provider),
			config.WithRegion(s.region))
		if err != nil {
			return fmt.Errorf("s3: couldn't load aws config: %w", err)
		}
		cfg = candidate
	}

	s.client = s3.NewFromConfig(cfg)

	// Check if bucket exists
	input := &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	}
	if _, err := s.client.HeadBucket(ctx, input); err != nil {
		return fmt.Errorf("s3: couldn't head bucket %s: %w", s.bucket, err)
	}

	return nil
}

func (s *Store) URL(ctx context.Context, name string) (string, error) {
	client := s3.NewPresignClient(s.client)
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(name),
	}
	presignedURL, err := client.PresignGetObject(ctx, input, s3.WithPresignExpires(24*time.Hour))
	if err != nil {
		return "", fmt.Errorf("s3: couldn't presign object %s: %w", name, err)
	}
	return presignedURL.URL, nil
}

func (s *Store) Upload(ctx context.Context, path, name string) error {
	var contentType string
	ext := filepath.Ext(path)
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".mp3":
		contentType = "audio/mpeg"
	case ".wav":
		contentType = "audio/wav"
	default:
		return fmt.Errorf("s3: unknown content type for extension %s", ext)
	}
	reader, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("s3: couldn't open file %s: %w", path, err)
	}
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(name),
		Body:        reader,
		ContentType: aws.String(contentType),
		// TODO: public or private?
		//GrantRead: aws.String("uri=http://acs.amazonaws.com/groups/global/AllUsers"),
	}

	out, err := s.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3: couldn't put object %s: %w", name, err)
	}
	if s.debug {
		js, _ := json.Marshal(out)
		log.Println("s3: put object", name, string(js))
	}
	return nil
}

var backoff = []time.Duration{
	15 * time.Second,
	30 * time.Second,
	1 * time.Minute,
}

func (s *Store) Download(ctx context.Context, path, name string) error {
	u, err := s.URL(ctx, name)
	if err != nil {
		return err
	}

	// Download file
	maxAttempts := 3
	attempts := 0
	var b []byte
	for {
		b, err = s.download(name, u)
		if err == nil {
			break
		}

		// Increase attempts and check if we should stop
		attempts++
		if attempts >= maxAttempts {
			return err
		}
		idx := attempts - 1
		if idx >= len(backoff) {
			idx = len(backoff) - 1
		}
		wait := backoff[idx]
		t := time.NewTimer(wait)
		if s.debug {
			log.Printf("%v (retrying in %s)\n", err, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}

	// Write to output
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("tgstore: couldn't write %s: %w", path, err)
	}
	return nil
}

func (s *Store) download(name, u string) ([]byte, error) {
	resp, err := s.httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("tgstore: couldn't download %s: %w", name, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tgstore: couldn't read %s: %w", name, err)
	}
	return b, nil
}

func (s *Store) Delete(ctx context.Context, name string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(name),
	}

	out, err := s.client.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3: couldn't delete object %s: %w", name, err)
	}

	if s.debug {
		js, _ := json.Marshal(out)
		log.Println("s3: delete object", name, string(js))
	}
	return nil
}
