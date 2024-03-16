package distrokid

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/go-rod/stealth"
	"github.com/igolaizola/musikai/pkg/ratelimit"
	"github.com/igolaizola/musikai/pkg/session"
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
}

type BrowserConfig struct {
	Wait        time.Duration
	Remote      string
	Proxy       string
	Profile     bool
	CookieStore CookieStore
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
	}
}

func (b *Browser) Start(parent context.Context) error {
	// Obtain the cookie
	rawCookies, err := b.cookieStore.GetCookie(parent)
	if err != nil {
		return err
	}
	if rawCookies == "" {
		return fmt.Errorf("distrokid: cookie is empty")
	}
	cookies, err := session.UnmarshalCookies(rawCookies, nil)
	if err != nil {
		return fmt.Errorf("distrokid: couldn't parse cookie: %w", err)
	}

	var browserContext, allocatorContext context.Context
	var browserCancel, allocatorCancel context.CancelFunc

	// Create a new context
	if b.remote != "" {
		log.Println("distrokid: connecting to browser at", b.remote)
		allocatorContext, allocatorCancel = chromedp.NewRemoteAllocator(context.Background(), b.remote)
	} else {
		log.Println("distrokid: launching browser")
		opts := append(
			chromedp.DefaultExecAllocatorOptions[3:],
			chromedp.NoFirstRun,
			chromedp.NoDefaultBrowserCheck,
			chromedp.Flag("headless", false),
		)

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

	// Launch stealth plugin
	if err := chromedp.Run(
		browserContext,
		chromedp.Evaluate(stealth.JS, nil),
	); err != nil {
		return fmt.Errorf("distrokid: could not launch stealth plugin: %w", err)
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
				err := network.SetCookie(cookie.Name, cookie.Value).
					WithDomain("distrokid.com").
					//WithHTTPOnly(true).
					Do(ctx)
				if err != nil {
					return fmt.Errorf("distrokid: could not set cookie: %w", err)
				}
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("distrokid: could not set cookie and navigate: %w", err)
	}

	if err := chromedp.Run(browserContext,
		// Load google first to have a sane referer
		chromedp.Navigate("https://www.google.com/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Navigate("https://distrokid.com/profile/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("distrokid: could not obtain chatgpt data: %w", err)
	}

	// Obtain the document
	var html string
	if err := chromedp.Run(browserContext,
		chromedp.OuterHTML("html", &html),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't get html: %w", err)
	}

	// Get user ID
	userID, err := getUserID(html)
	if err != nil {
		return fmt.Errorf("distrokid: couldn't get user ID: %w", err)
	}
	fmt.Println("user id:", userID)

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
			candidate, err := network.GetCookies().WithUrls([]string{"https://distrokid.com"}).Do(ctx)
			if err != nil {
				return fmt.Errorf("distrokid: could not get cookies: %w", err)
			}
			cs = candidate
			return nil
		}),
	); err != nil {
		return fmt.Errorf("distrokid: could not get cookies: %w", err)
	}

	// Set the cookie
	var cookies []*http.Cookie
	for _, cookie := range cs {
		cookies = append(cookies, &http.Cookie{
			Name:  cookie.Name,
			Value: cookie.Value,
		})
	}
	raw := session.MarshalCookies(cookies)
	if err := c.cookieStore.SetCookie(c.browserContext, raw); err != nil {
		return fmt.Errorf("distrokid: couldn't set cookie: %w", err)
	}
	return nil
}

func getUserID(html string) (int, error) {
	// Find the first match in the HTML content
	varPattern := regexp.MustCompile(`(?s)var me\s*=\s*({.*?});`)
	matches := varPattern.FindStringSubmatch(html)
	if len(matches) < 2 {
		return 0, fmt.Errorf("distrokid: couldn't find me object")
	}
	// Extract the javascript object
	js := matches[1]

	// Replace single quotes with double quotes
	js = strings.ReplaceAll(js, "'", "\"")
	// Add quotes around keys
	js = strings.ReplaceAll(js, "://", "$//")
	re := regexp.MustCompile(`(\w+)\s*:`)
	js = re.ReplaceAllString(js, `"$1":`)
	js = strings.ReplaceAll(js, "$//", "://")
	// Remove last comma
	re = regexp.MustCompile(`\s*(,)\s*}`)
	js = re.ReplaceAllString(js, "\n}")

	var me meResponse
	if err := json.Unmarshal([]byte(js), &me); err != nil {
		return 0, fmt.Errorf("distrokid: couldn't unmarshal me response: %w", err)
	}
	if me.ID == 0 {
		return 0, fmt.Errorf("distrokid: couldn't find user ID")
	}
	return me.ID, nil
}
