package inkpic

import (
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/chromedp/chromedp"
)

// Server serves the inkpic server.
func Serve(ctx context.Context, port int) error {
	log.Printf("server listening on port %d\n", port)
	<-ctx.Done()
	return nil
}

// Run runs the inkpic process.
func Run(ctx context.Context) error {
	log.Println("running")
	defer log.Println("finished")
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
	return nil
}

// Default values
// Width    = "90%"
// FontSize = "8vw"
// Shadow   = "0 0 1vw rgba(0, 0, 0, 0.5), 0 0 2vw rgba(0, 0, 0, 0.5), 0 0 3vw rgba(0, 0, 0, 0.5), 0 0 4vw rgba(0, 0, 0, 0.5)"
const (
	Width    = "90%"
	FontSize = "9vw"
	Shadow   = "0 0 1vw rgba(0, 0, 0, 0.5), 0 0 2vw rgba(0, 0, 0, 0.5), 0 0 3vw rgba(0, 0, 0, 0.5), 0 0 4vw rgba(0, 0, 0, 0.5)"
)

const tpl = `
<!DOCTYPE html>
<html lang="en">

<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        @font-face {
            font-family: 'InterBold';
            src: url('/asset/{{ .Font }}') format('truetype');
        }

        body,
        html {
            margin: 0;
            padding: 0;
            height: 100%;
            width: 100%;
            display: flex;
            justify-content: center;
            align-items: center;
            background: url('/asset/{{ .Image }}') no-repeat center center;
            background-size: cover;
            font-family: 'InterBold', sans-serif;
        }

        .text-container {
            position: relative;
            width: {{ .Width }};
            text-align: center;
        }

        .centered-text {
            font-size: {{ .FontSize }};
            background: linear-gradient(180deg, #{{ .Color1 }}, #{{ .Color2 }});
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            position: relative;
            z-index: 1;
            display: inline-block;
        }

        .text-shadow {
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            z-index: 0;
            color: transparent;
            text-shadow: {{ .Shadow }};
            font-size: {{ .FontSize }};
            display: flex;
            justify-content: center;
            align-items: center;
        }

        .text-content {
            position: relative;
            display: inline-block;
        }
    </style>
</head>

<body>
    <div class="text-container">
        <div class="text-content">
            <div class="text-shadow">{{ .Text }}</div>
            <div class="centered-text">{{ .Text }}</div>
        </div>
    </div>
</body>

</html>
`

type coverData struct {
	Font     string
	Image    string
	Text     string
	Color1   string
	Color2   string
	Width    string
	FontSize string
	Shadow   template.CSS
}

type Client struct {
	browserContext   context.Context
	allocatorContext context.Context
	browserCancel    context.CancelFunc
	allocatorCancel  context.CancelFunc
	serverURL        string
}

func New(parent context.Context) (*Client, error) {
	opts := append(
		chromedp.DefaultExecAllocatorOptions[3:],
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", true),
	)
	allocatorContext, allocatorCancel := chromedp.NewExecAllocator(context.Background(), opts...)

	// create chrome instance
	browserContext, browserCancel := chromedp.NewContext(
		allocatorContext,
		// chromedp.WithDebugf(log.Printf),
	)

	// Parse the HTML template
	tmpl, err := template.New("tmpl").Parse(tpl)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse template: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/asset/{slug}", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		if slug == "" {
			http.Error(w, "missing slug", http.StatusBadRequest)
			return
		}
		path, err := base64.RawURLEncoding.DecodeString(slug)
		if err != nil {
			http.Error(w, "invalid slug", http.StatusBadRequest)
			return
		}
		http.ServeFile(w, r, string(path))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Obtain font data from query
		font := r.URL.Query().Get("font")
		if font == "" {
			http.Error(w, "missing font query parameter", http.StatusBadRequest)
			return
		}
		size := r.URL.Query().Get("size")
		if size == "" {
			size = FontSize
		}
		image := r.URL.Query().Get("image")
		if image == "" {
			http.Error(w, "missing image query parameter", http.StatusBadRequest)
			return
		}
		textB64 := r.URL.Query().Get("text")
		if textB64 == "" {
			http.Error(w, "missing text query parameter", http.StatusBadRequest)
			return
		}
		textBytes, err := base64.RawURLEncoding.DecodeString(textB64)
		if err != nil {
			http.Error(w, "invalid text query parameter", http.StatusBadRequest)
			return
		}
		text := string(textBytes)

		// Data to be passed to the template
		gradient := randomGradient()
		data := coverData{
			Font:     font,
			Image:    image,
			Text:     text,
			Color1:   gradient[0],
			Color2:   gradient[1],
			Width:    Width,
			FontSize: size,
			Shadow:   template.CSS(Shadow),
		}
		w.Header().Set("Content-Type", "text/html")
		if err := tmpl.Execute(w, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Listen on a random port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, fmt.Errorf("couldn't listen on a port: %w", err)
	}
	srv := &http.Server{
		Addr:    listener.Addr().String(),
		Handler: mux,
	}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Serve(): %s", err)
		}
	}()

	// Retrieve the actual port assigned
	port := listener.Addr().(*net.TCPAddr).Port
	serverURL := fmt.Sprintf("http://localhost:%d", port)

	go func() {
		<-parent.Done()
		_ = srv.Shutdown(context.Background())
		browserCancel()
		allocatorCancel()
		go func() {
			_ = chromedp.Cancel(browserContext)
		}()
	}()

	return &Client{
		browserContext:   browserContext,
		browserCancel:    browserCancel,
		allocatorContext: allocatorContext,
		allocatorCancel:  allocatorCancel,
		serverURL:        serverURL,
	}, nil
}

func (c *Client) AddText(parent context.Context, imagePath, text, fontPath, fontSize string, outputImagePath string) error {
	// Get the image dimensions
	file, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("couldn't open image: %w", err)
	}
	defer file.Close()

	img, _, err := image.DecodeConfig(file)
	if err != nil {
		return fmt.Errorf("couldn't decode image %s: %w", imagePath, err)
	}

	width := img.Width
	height := img.Height

	// Create a new tab based on client context
	ctx, cancel := chromedp.NewContext(c.browserContext)
	defer cancel()

	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	// Add query parameters to the server URL
	u := fmt.Sprintf("%s/?font=%s&size=%s&image=%s&text=%s", c.serverURL,
		base64.RawURLEncoding.EncodeToString([]byte(fontPath)),
		fontSize,
		base64.RawURLEncoding.EncodeToString([]byte(imagePath)),
		base64.RawURLEncoding.EncodeToString([]byte(text)))

	// Run the task list
	var buf []byte
	if err := chromedp.Run(ctx, screenshot(u, width, height, &buf)); err != nil {
		return err
	}

	// Save the screenshot to a file
	if err := os.WriteFile(outputImagePath, buf, 0644); err != nil {
		return err
	}

	// Obtain the document
	/*
		var html string
		if err := chromedp.Run(ctx,
			chromedp.OuterHTML("html", &html),
		); err != nil {
			return fmt.Errorf("couldn't get html: %w", err)
		}
		ext := filepath.Ext(outputImagePath)
		outputHTMLPath := strings.TrimSuffix(outputImagePath, ext) + ".html"
		if err := os.WriteFile(outputHTMLPath, []byte(html), 0644); err != nil {
			return err
		}
	*/

	return nil
}

func screenshot(urlstr string, width, height int, res *[]byte) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.Navigate(urlstr),
		chromedp.EmulateViewport(int64(width), int64(height)),
		chromedp.WaitVisible(`body`, chromedp.ByQuery),
		chromedp.Screenshot(`body`, res, chromedp.NodeVisible, chromedp.ByQuery),
	}
}

var gradients [][2]string = [][2]string{
	//{"cf8bf3", "fdb99b"}, // Purple to Peach
	{"87ceeb", "00bfff"}, // Sky Blue to Deep Sky Blue
	{"ff69b4", "ffb6c1"}, // Hot Pink to Light Pink
	//{"228b22", "90ee90"}, // Forest Green to Light Green
	{"ff8c00", "ffd700"}, // Dark Orange to Gold
	{"ff6347", "ffa07a"}, // Tomato to Light Salmon
	{"4b0082", "8a2be2"}, // Indigo to Blue Violet
	{"00ffff", "7fffd4"}, // Aqua to Aquamarine
}

func randomGradient() [2]string {
	rnd := rand.Intn(len(gradients))
	return gradients[rnd]
}
