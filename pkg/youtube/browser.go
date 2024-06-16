package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/go-rod/stealth"
	"github.com/igolaizola/musikai/pkg/ratelimit"
)

type Browser struct {
	parent           context.Context
	browserContext   context.Context
	allocatorContext context.Context
	browserCancel    context.CancelFunc
	allocatorCancel  context.CancelFunc
	rateLimit        ratelimit.Lock
	remote           string
	proxy            string
	profile          bool
	cookieStore      CookieStore
	binPath          string
	channelID        string
	channelName      string
}

type BrowserConfig struct {
	Wait        time.Duration
	Remote      string
	Proxy       string
	Profile     bool
	CookieStore CookieStore
	BinPath     string
	ChannelID   string
	ChannelName string
}

func NewBrowser(cfg *BrowserConfig) *Browser {
	wait := cfg.Wait
	if wait == 0 {
		wait = 1 * time.Second
	}
	return &Browser{
		remote:      cfg.Remote,
		proxy:       cfg.Proxy,
		profile:     cfg.Profile,
		cookieStore: cfg.CookieStore,
		rateLimit:   ratelimit.New(wait),
		binPath:     cfg.BinPath,
		channelID:   cfg.ChannelID,
		channelName: cfg.ChannelName,
	}
}

func (b *Browser) Start(parent context.Context) error {
	// Obtain the cookie
	rawCookies, err := b.cookieStore.GetCookie(parent)
	if err != nil {
		return err
	}

	var cookies []*network.Cookie
	if rawCookies != "" {
		var candidate []*network.Cookie
		if err := json.Unmarshal([]byte(rawCookies), &candidate); err != nil {
			log.Println("youtube: couldn't unmarshal cookie:", err)
		} else {
			cookies = candidate
		}
	}

	var browserContext, allocatorContext context.Context
	var browserCancel, allocatorCancel context.CancelFunc

	// Create a new context
	if b.remote != "" {
		log.Println("youtube: connecting to browser at", b.remote)
		allocatorContext, allocatorCancel = chromedp.NewRemoteAllocator(context.Background(), b.remote)
	} else {
		log.Println("youtube: launching browser")
		opts := append(
			chromedp.DefaultExecAllocatorOptions[3:],
			chromedp.NoFirstRun,
			chromedp.NoDefaultBrowserCheck,
			chromedp.Flag("headless", false),
		)

		if b.binPath != "" {
			opts = append(opts,
				chromedp.ExecPath(b.binPath),
			)
		}

		if b.proxy != "" {
			opts = append(opts,
				chromedp.ProxyServer(b.proxy),
			)
		}

		if b.profile {
			opts = append(opts,
				// if user-data-dir is set, chrome won't load the default profile,
				// even if it's set to the directory where the default profile is stored.
				// set it to empty to prevent chromedp from setting it to a temp directory.
				chromedp.UserDataDir(""),
				chromedp.Flag("disable-extensions", false),
			)
		}
		allocatorContext, allocatorCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	}

	// create chrome instance
	browserContext, browserCancel = chromedp.NewContext(
		allocatorContext,
		// chromedp.WithDebugf(log.Printf),
	)

	// Cancel the browser context when the parent is done
	go func() {
		select {
		case <-parent.Done():
		case <-browserContext.Done():
			return
		}
		browserCancel()
		allocatorCancel()
	}()

	// Launch stealth plugin
	if err := chromedp.Run(
		browserContext,
		chromedp.Evaluate(stealth.JS, nil),
	); err != nil {
		return fmt.Errorf("youtube: could not launch stealth plugin: %w", err)
	}

	// disable webdriver
	if err := chromedp.Run(browserContext, chromedp.ActionFunc(func(cxt context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument("Object.defineProperty(navigator, 'webdriver', { get: () => false, });").Do(cxt)
		if err != nil {
			return err
		}
		return nil
	})); err != nil {
		return fmt.Errorf("could not disable webdriver: %w", err)
	}

	// Actions to set the cookie and navigate
	if err := chromedp.Run(browserContext,
		chromedp.ActionFunc(func(ctx context.Context) error {
			for _, cookie := range cookies {
				if err := network.SetCookie(cookie.Name, cookie.Value).
					WithDomain(cookie.Domain).
					WithSecure(cookie.Secure).
					WithHTTPOnly(cookie.HTTPOnly).
					WithSameSite(cookie.SameSite).
					Do(ctx); err != nil {
					return fmt.Errorf("youtube: could not set cookie (%s: %s): %w", cookie.Name, cookie.Value, err)
				}
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("youtube: could not set cookie and navigate: %w", err)
	}

	if err := chromedp.Run(browserContext,
		// Load google first to have a sane referer
		chromedp.Navigate("https://www.google.com/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Navigate(fmt.Sprintf("https://studio.youtube.com/channel/%s", b.channelID)),
		chromedp.WaitReady("#entity-name", chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("youtube: couldn't get channel: %w", err)
	}

	// Obtain the document
	var html string
	if err := chromedp.Run(browserContext,
		chromedp.OuterHTML("html", &html),
	); err != nil {
		return fmt.Errorf("youtube: couldn't get html: %w", err)
	}

	// Search for handle
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return fmt.Errorf("youtube: couldn't parse html: %w", err)
	}
	var handle string
	doc.Find("#entity-name").Each(func(i int, s *goquery.Selection) {
		handle = s.Text()
	})
	handle = strings.TrimPrefix(handle, "@")
	handle = strings.ReplaceAll(handle, " ", "")
	handle = strings.ToLower(handle)

	if handle == "" {
		return fmt.Errorf("youtube: couldn't find handle")
	}
	if strings.Contains(handle, b.channelName) {
		// TODO: fix handle
		log.Println(fmt.Errorf("youtube: invalid handle %q, %s", handle, b.channelName))
	}

	b.browserContext = browserContext
	b.browserCancel = browserCancel
	b.allocatorContext = allocatorContext
	b.allocatorCancel = allocatorCancel
	b.parent = parent

	return nil
}

// Stop closes the browser.
func (c *Browser) Stop() error {
	defer func() {
		c.browserCancel()
		c.allocatorCancel()
		go func() {
			_ = chromedp.Cancel(c.browserContext)
		}()
	}()

	// Obtain cookies after navigation
	var cs []*network.Cookie
	if err := chromedp.Run(c.browserContext,
		chromedp.ActionFunc(func(ctx context.Context) error {
			candidate, err := network.GetCookies().WithUrls([]string{"https://youtube.com"}).Do(ctx)
			if err != nil {
				return fmt.Errorf("youtube: could not get cookies: %w", err)
			}
			cs = candidate
			return nil
		}),
	); err != nil {
		return fmt.Errorf("youtube: could not get cookies: %w", err)
	}

	// Set the cookie
	raw, err := json.Marshal(cs)
	if err != nil {
		return fmt.Errorf("youtube: couldn't marshal cookies: %w", err)
	}
	if err := c.cookieStore.SetCookie(c.browserContext, string(raw)); err != nil {
		return fmt.Errorf("youtube: couldn't set cookie: %w", err)
	}
	return nil
}
