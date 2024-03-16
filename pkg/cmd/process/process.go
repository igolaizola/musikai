package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/filestorage/tgstore"
	"github.com/igolaizola/musikai/pkg/sound"
	"github.com/igolaizola/musikai/pkg/sound/aubio"
	"github.com/igolaizola/musikai/pkg/sound/ffmpeg"
	"github.com/igolaizola/musikai/pkg/sound/phaselimiter"
	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug       bool
	DBType      string
	DBConn      string
	Timeout     time.Duration
	Concurrency int
	Limit       int
	Proxy       string

	S3Bucket string
	S3Region string
	S3Key    string
	S3Secret string

	TGChat  int64
	TGToken string

	Type         string
	Reprocess    bool
	ShortFadeOut time.Duration
	LongFadeOut  time.Duration
}

// Run launches the gen generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	action := "process"
	if cfg.Reprocess {
		action = "reprocess"
	}
	log.Printf("process: %s started\n", action)
	defer func() {
		log.Printf("process: %s ended (%d)\n", action, iteration)
	}()

	debug := func(format string, args ...any) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	if cfg.ShortFadeOut == 0 {
		return errors.New("process: short fade out is required")
	}
	if cfg.LongFadeOut == 0 {
		return errors.New("process: long fade out is required")
	}
	if cfg.ShortFadeOut > cfg.LongFadeOut {
		return errors.New("process: short fade out must be less than long fade out")
	}

	if _, err := aubio.Version(ctx); err != nil {
		return fmt.Errorf("process: couldn't get aubio version: %w", err)
	}

	ph := phaselimiter.New(&phaselimiter.Config{})
	if _, err := ph.Version(ctx); err != nil {
		return fmt.Errorf("process: couldn't get phaselimiter version: %w", err)
	}

	// TODO: Allow to use S3 storage
	/*
		s3Store := s3.New(cfg.S3Key, cfg.S3Secret, cfg.S3Region, cfg.S3Bucket, cfg.Debug)
		if err := s3Store.Start(ctx); err != nil {
			return fmt.Errorf("process: couldn't start s3 store: %w", err)
		}
	*/

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("process: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("process: couldn't start orm store: %w", err)
	}

	tgStore, err := tgstore.New(cfg.TGToken, cfg.TGChat, cfg.Proxy, cfg.Debug)
	if err != nil {
		return fmt.Errorf("process: couldn't create file storage: %w", err)
	}
	var tgLock sync.Mutex

	httpClient := &http.Client{
		Timeout: 2 * time.Minute,
	}
	if cfg.Proxy != "" {
		u, err := url.Parse(cfg.Proxy)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}
		httpClient.Transport = &http.Transport{
			Proxy: http.ProxyURL(u),
		}
	}

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("process: total time %s, average time %s\n", total, total/time.Duration(iteration))
	}()

	nErr := 0
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 24 * time.Hour
	}
	ticker := time.NewTicker(timeout)
	last := time.Now()
	defer ticker.Stop()

	// Concurrency settings
	concurrency := cfg.Concurrency
	if concurrency == 0 {
		concurrency = 1
	}
	errC := make(chan error, concurrency)
	defer close(errC)
	for i := 0; i < concurrency; i++ {
		errC <- nil
	}
	var wg sync.WaitGroup
	defer wg.Wait()

	// Phase limiter lock to avoid concurrent calls
	var phLock sync.Mutex

	var gens []*storage.Generation
	var currID string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("process: %w", ctx.Err())
		case <-ticker.C:
			return nil
		case err := <-errC:
			if err != nil {
				nErr += 1
			} else {
				nErr = 0
			}

			// Check exit conditions
			if nErr > 10 {
				return fmt.Errorf("process: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("process: iteration %d\n", iteration)
			}

			// Get next generation
			filters := []storage.Filter{
				storage.Where("processed = ?", cfg.Reprocess),
				storage.Where("id > ?", currID),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
			}

			// Get next image
			if len(gens) == 0 {
				// Get a generations
				gens, err = store.ListGenerations(ctx, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("process: couldn't get generation from database: %w", err)
				}
				if len(gens) == 0 {
					return errors.New("process: no generations to process")
				}
				currID = gens[len(gens)-1].ID
			}
			gen := gens[0]
			gens = gens[1:]

			// Launch process in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("process: start %s", gen.ID)
				var err error
				if cfg.Reprocess {
					err = reprocess(ctx, gen, debug, store, tgStore)
				} else {
					err = process(ctx, gen, debug, store, tgStore, &tgLock, httpClient, ph, &phLock, cfg.ShortFadeOut, cfg.LongFadeOut)
				}
				if err != nil {
					log.Println(err)
				}
				debug("process: end %s", gen.ID)
				errC <- err
			}()
		}
	}
}

type flags struct {
	Silences []int `json:"silences,omitempty"`
	Short    bool  `json:"short,omitempty"`
	BPM2     bool  `json:"bpm_2,omitempty"`
	BPM4     bool  `json:"bpm_4,omitempty"`
	BPMN     bool  `json:"bpm_n,omitempty"`
}

func process(ctx context.Context, gen *storage.Generation, debug func(string, ...any), store *storage.Store, tgStore *tgstore.Store, tgLock *sync.Mutex,
	client *http.Client, ph *phaselimiter.PhaseLimiter, phLock *sync.Mutex, shortFadeOut, longFadeOut time.Duration) error {

	// Download the audio file
	debug("process: start download %s", gen.ID)
	b, err := download(ctx, client, gen.SunoAudio)
	if err != nil {
		return fmt.Errorf("process: couldn't download gen audio: %w", err)
	}
	original := filepath.Join(os.TempDir(), fmt.Sprintf("%s.mp3", gen.ID))
	defer func() { _ = os.Remove(original) }()
	if err := os.WriteFile(original, b, 0644); err != nil {
		return fmt.Errorf("process: couldn't save gen audio: %w", err)
	}
	debug("process: end download %s", gen.ID)

	// Create master folder if it doesn't exist
	masterDir := filepath.Join(os.TempDir(), "master")
	if err := os.MkdirAll(masterDir, 0755); err != nil {
		return fmt.Errorf("process: couldn't create master folder: %w", err)
	}
	mastered := filepath.Join(masterDir, fmt.Sprintf("%s.mp3", gen.ID))

	// Master the gens
	if _, err := os.Stat(mastered); err == nil {
		if err := os.Remove(mastered); err != nil {
			return fmt.Errorf("process: couldn't remove old master: %w", err)
		}
	}
	debug("process: start master %s", gen.ID)
	if err := func() error {
		// Lock the phase limiter to avoid concurrent calls
		phLock.Lock()
		defer phLock.Unlock()
		if ctx.Err() != nil {
			return fmt.Errorf("process: %w", ctx.Err())
		}

		if err := ph.Master(ctx, original, mastered); err != nil {
			return fmt.Errorf("process: couldn't master gen: %w", err)
		}
		return nil
	}(); err != nil {
		return err
	}
	debug("process: end master %s", gen.ID)

	// Create analyzer to get silences
	debug("process: start cut and fade out %s", gen.ID)
	analyzer, err := sound.NewAnalyzer(mastered)
	if err != nil {
		return fmt.Errorf("process: couldn't create analyzer: %w", err)
	}
	silences, err := analyzer.Silences(ctx)
	if err != nil {
		return fmt.Errorf("process: couldn't get silences: %w", err)
	}

	fadeOut := longFadeOut
	var ends bool

	// Remove last silence
	if len(silences) > 0 {
		last := silences[len(silences)-1]
		if last.Final || last.End > analyzer.Duration()-10*time.Second {
			// Cut the last silence
			if err := ffmpeg.Cut(ctx, mastered, mastered, last.Start); err != nil {
				return fmt.Errorf("process: couldn't cut last silence: %w", err)
			}
		}
		fadeOut = shortFadeOut
		ends = true
	}

	// Apply fade out
	if err := ffmpeg.FadeOut(ctx, mastered, mastered, analyzer.Duration(), fadeOut); err != nil {
		return fmt.Errorf("process: couldn't fade out gen: %w", err)
	}
	debug("process: end cut and fade out %s", gen.ID)

	analyzer, err = sound.NewAnalyzer(mastered)
	if err != nil {
		return fmt.Errorf("process: couldn't create analyzer: %w", err)
	}

	// process the wave image
	waveBytes, err := analyzer.PlotWave(gen.Song.Style)
	if err != nil {
		return fmt.Errorf("process: couldn't plot wave: %w", err)
	}
	wavePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s.jpg", gen.ID))
	if err := os.WriteFile(wavePath, waveBytes, 0644); err != nil {
		return fmt.Errorf("process: couldn't write wave image: %w", err)
	}
	defer func() { _ = os.Remove(wavePath) }()

	debug("process: start upload %s", gen.ID)
	var masterID string
	var waveID string
	if err := func() error {
		// Lock the tg store to avoid concurrent calls
		tgLock.Lock()
		defer tgLock.Unlock()
		if ctx.Err() != nil {
			return fmt.Errorf("process: %w", ctx.Err())
		}

		// Upload the wave image
		waveID, err = tgStore.Set(ctx, wavePath)
		if err != nil {
			return fmt.Errorf("process: couldn't save wave image to telegram: %w", err)
		}

		// Upload the mastered audio
		masterID, err = tgStore.Set(ctx, mastered)
		if err != nil {
			return fmt.Errorf("process: couldn't save mastered audio to telegram: %w", err)
		}

		return nil
	}(); err != nil {
		return err
	}
	debug("process: end upload %s", gen.ID)

	// Get the tempo
	tempo, err := aubio.Tempo(ctx, mastered)
	if err != nil {
		return fmt.Errorf("process: couldn't get tempo: %w", err)
	}
	return processFlags(ctx, gen, mastered, ends, float32(tempo), masterID, waveID, analyzer, debug, store)
}

func processFlags(ctx context.Context, gen *storage.Generation, mastered string, ends bool,
	tempo float32, masterID string, waveID string, analyzer *sound.Analyzer,
	debug func(string, ...any), store *storage.Store) error {

	// Reload analyzer to process flags
	debug("process: start flags %s", gen.ID)

	// Get the silences again
	silences, err := analyzer.Silences(ctx)
	if err != nil {
		return fmt.Errorf("process: couldn't get silences: %w", err)
	}

	// Detect flags
	f := flags{}
	for _, s := range silences {
		// If the silence is final, don't add it
		if s.Final {
			break
		}
		// If the silence is near the end, don't add it (it's probably a fade out)
		if s.End > analyzer.Duration()-5*time.Second {
			break
		}
		p := (s.Start.Seconds() + s.Duration.Seconds()/2.0) / analyzer.Duration().Seconds()
		p100 := int(p * 100.0)
		f.Silences = append(f.Silences, p100)
	}

	// Short gen
	if analyzer.Duration() < 2*time.Minute {
		f.Short = true
	}

	// BPM changes
	beats, err := aubio.BPM(ctx, mastered)
	if err != nil {
		return fmt.Errorf("process: couldn't get bpm: %w", err)
	}

	f.BPM2 = analyzer.BPMChange(beats, []float64{analyzer.Duration().Seconds() / 2.0})

	q := analyzer.Duration().Seconds() / 4.0
	f.BPM4 = analyzer.BPMChange(beats, []float64{1 * q, 2 * q, 3 * q})

	noises, err := analyzer.Noises(ctx)
	if err != nil {
		return fmt.Errorf("process: couldn't get noises: %w", err)
	}
	f.BPMN = analyzer.FragmentBPMChange(beats, noises)

	flagsBytes, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("process: couldn't marshal flags: %w", err)
	}
	flagJSON := string(flagsBytes)

	debug("process: end flags %s", gen.ID)
	if flagJSON == "{}" {
		flagJSON = ""
	}

	// Get the latest version of the gen
	gen, err = store.GetGeneration(ctx, gen.ID)
	if err != nil {
		return fmt.Errorf("process: couldn't get gen from database: %w", err)
	}

	// Update the gen
	gen.Master = masterID
	gen.Wave = waveID
	gen.Tempo = float32(tempo)
	gen.Processed = true
	gen.Duration = float32(analyzer.Duration().Seconds())
	gen.Ends = ends
	gen.Flags = flagJSON
	gen.Flagged = flagJSON != ""

	debug("flags: %s", flagJSON)

	if err := store.SetGeneration(ctx, gen); err != nil {
		return fmt.Errorf("process: couldn't save gen to database: %w", err)
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("couldn't create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't download video: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("couldn't read response body: %w", err)
	}
	return b, nil
}

func reprocess(ctx context.Context, gen *storage.Generation, debug func(string, ...any), store *storage.Store, tgStore *tgstore.Store) error {
	// Download the mastered audio
	debug("process: start download master %s", gen.ID)
	mastered := filepath.Join(os.TempDir(), fmt.Sprintf("%s.mp3", gen.ID))
	if err := tgStore.Download(ctx, gen.Master, mastered); err != nil {
		return fmt.Errorf("process: couldn't download master audio: %w", err)
	}
	debug("process: end download master %s", gen.ID)
	f := flags{}
	if gen.Flags != "" {
		if err := json.Unmarshal([]byte(gen.Flags), &f); err != nil {
			return fmt.Errorf("process: couldn't unmarshal flags: %w", err)
		}
	}
	analyzer, err := sound.NewAnalyzer(mastered)
	if err != nil {
		return fmt.Errorf("process: couldn't create analyzer: %w", err)
	}
	return processFlags(ctx, gen, mastered, gen.Ends, gen.Tempo, gen.Master, gen.Wave, analyzer, debug, store)
}
