package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// New returns a new S3 image store.
func New(key, secret, region, bucket string, debug bool) *Store {
	return &Store{
		key:    key,
		secret: secret,
		region: region,
		bucket: bucket,
		debug:  debug,
	}
}

type Store struct {
	key    string
	secret string
	region string
	bucket string
	debug  bool
	client *s3.Client
}

func (s *Store) URL(file string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, file)
}

func (s *Store) Start(ctx context.Context) error {
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

func (s *Store) GetImage(ctx context.Context, key string) (string, error) {
	client := s3.NewPresignClient(s.client)
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	presignedURL, err := client.PresignGetObject(ctx, input, s3.WithPresignExpires(24*time.Hour))
	if err != nil {
		return "", fmt.Errorf("s3: couldn't presign object %s: %w", key, err)
	}
	return presignedURL.URL, nil
}

func (s *Store) SetImage(ctx context.Context, key string, value []byte) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		// TODO: extension should be part of the key
		Key:         aws.String(key),
		Body:        bytes.NewReader(value),
		ContentType: aws.String("image/jpeg"),
		// TODO: public or private?
		//GrantRead: aws.String("uri=http://acs.amazonaws.com/groups/global/AllUsers"),
	}

	out, err := s.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3: couldn't put object %s: %w", key, err)
	}
	if s.debug {
		js, _ := json.Marshal(out)
		log.Println("s3: put object", key, string(js))
	}
	return nil
}

func (s *Store) DeleteImage(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	out, err := s.client.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3: couldn't delete object %s: %w", key, err)
	}

	if s.debug {
		js, _ := json.Marshal(out)
		log.Println("s3: delete object", key, string(js))
	}
	return nil
}
