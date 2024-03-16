package distrokid

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

type Album struct {
	Artist         string
	FirstName      string
	LastName       string
	Title          string
	PrimaryGenre   string
	SecondaryGenre string
	Cover          string
	Songs          []*Song
}

type Song struct {
	Instrumental bool
	Title        string
	File         string
}

func (a *Album) Validate() error {
	if a.Artist == "" {
		return fmt.Errorf("distrokid: artist is empty")
	}
	if a.Title == "" {
		return fmt.Errorf("distrokid: title is empty")
	}
	if a.FirstName == "" {
		return fmt.Errorf("distrokid: first name is empty")
	}
	if a.LastName == "" {
		return fmt.Errorf("distrokid: last name is empty")
	}
	if a.PrimaryGenre == "" {
		return fmt.Errorf("distrokid: primary genre is empty")
	}
	if len(a.Songs) == 0 {
		return fmt.Errorf("distrokid: no songs")
	}
	if a.Cover == "" {
		return fmt.Errorf("distrokid: cover is empty")
	}
	if _, err := os.Stat(a.Cover); os.IsNotExist(err) {
		return fmt.Errorf("distrokid: cover file doesn't exist: %s", a.Cover)
	}
	for i, song := range a.Songs {
		if song.Title == "" {
			return fmt.Errorf("distrokid: song %d title is empty", i+1)
		}
		if song.File == "" {
			return fmt.Errorf("distrokid: song %d file is empty", i+1)
		}
		if _, err := os.Stat(song.File); os.IsNotExist(err) {
			return fmt.Errorf("distrokid: song %d file doesn't exist: %s", i+1, song.File)
		}
	}
	return nil
}

// Publish publishes a new album
func (c *Browser) Publish(parent context.Context, album *Album, auto bool) (string, error) {
	// Validate album
	if err := album.Validate(); err != nil {
		return "", err
	}

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

	// Navigate to the new album page
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://distrokid.com/new/"),
		chromedp.WaitVisible("body", chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("distrokid: couldn't navigate to url: %w", err)
	}

	// Change to english
	if err := selectOption(ctx, `#sitetran_select`, "en"); err != nil {
		return "", err
	}

	// Wait for the page to change the language
	time.Sleep(1 * time.Second)

	// Select the number of songs
	if err := selectOption(ctx, `#howManySongsOnThisAlbum`, fmt.Sprintf("%d", len(album.Songs))); err != nil {
		return "", err
	}

	// Wait for the page to reload
	time.Sleep(1 * time.Second)

	// Obtain the document
	doc, err := getHTML(ctx, "html")
	if err != nil {
		return "", err
	}

	// Get user ID
	html, err := doc.Html()
	if err != nil {
		return "", fmt.Errorf("distrokid: couldn't get html from doc: %w", err)
	}
	userID, err := getUserID(html)
	if err != nil {
		return "", fmt.Errorf("distrokid: couldn't get user ID: %w", err)
	}
	log.Println("user id:", userID)

	// Get album UUID
	albumUUID, err := getAlbumUUID(doc)
	if err != nil {
		return "", fmt.Errorf("distrokid: couldn't get albumuuid: %w", err)
	}
	log.Println("album uuid:", albumUUID)

	// Album parameters
	if err := setValue(ctx, "#artistName", album.Artist); err != nil {
		return "", err
	}

	// Obtain genre options
	genres := map[string]string{}
	var all []string
	doc.Find(fmt.Sprintf("%s option", "#genrePrimary")).Each(func(i int, s *goquery.Selection) {
		genre, ok := s.Attr("genre")
		if !ok {
			return
		}
		value, ok := s.Attr("value")
		if !ok {
			return
		}
		all = append(all, genre)
		genres[genre] = value
	})

	// Select primary and secondary genre
	primaryGenre, ok := genres[album.PrimaryGenre]
	if !ok {
		return "", fmt.Errorf("distrokid: couldn't find primary genre %s in %s", album.PrimaryGenre, strings.Join(all, ","))
	}
	if err := selectOption(ctx, "#genrePrimary", primaryGenre); err != nil {
		return "", err
	}
	if album.SecondaryGenre != "" {
		secondaryGenre, ok := genres[album.SecondaryGenre]
		if !ok {
			return "", fmt.Errorf("distrokid: couldn't find secondary genre %s in %s", album.SecondaryGenre, strings.Join(all, ","))
		}
		if err := selectOption(ctx, "#genreSecondary", secondaryGenre); err != nil {
			return "", err
		}
	}

	// Upload cover
	if err := upload(ctx, `#artwork`, album.Cover, "img.artworkPreview"); err != nil {
		return "", err
	}

	// Obtain the updated document
	doc, err = getHTML(ctx, "html")
	if err != nil {
		return "", err
	}

	if len(album.Songs) > 1 {
		if err := setValue(ctx, "#albumTitle", album.Title); err != nil {
			return "", err
		}
		// Obtain the highest album price
		if err := setMaxPrice(ctx, doc, "#priceAlbum"); err != nil {
			return "", err
		}
	}

	// Obtain the track IDs
	trackIDs := make([]string, len(album.Songs))
	doc.Find("input[name^=tracknum_]").Each(func(i int, s *goquery.Selection) {
		v, ok := s.Attr("value")
		if !ok {
			log.Println("couldn't find tracknum")
			return
		}
		num, err := strconv.Atoi(v)
		if err != nil {
			log.Println("couldn't parse tracknum")
			return
		}
		id, ok := s.Attr("id")
		if !ok {
			log.Println("couldn't find id")
			return
		}
		id = strings.TrimPrefix(id, "tracknum_")
		trackIDs[num-1] = id
	})
	for i, id := range trackIDs {
		if id == "" {
			return "", fmt.Errorf("distrokid: couldn't find track id for song %d", i+1)
		}
	}

	for i, song := range album.Songs {
		n := i + 1
		id := trackIDs[i]
		// Set song title
		if err := setValue(ctx, fmt.Sprintf("#title_%s", id), song.Title); err != nil {
			return "", err
		}
		// Upload song
		if err := upload(ctx, fmt.Sprintf("#js-track-upload-%d", n), song.File, fmt.Sprintf("#showFilename_%d", n)); err != nil {
			return "", err
		}

		// Set song writer
		if err := setValue(ctx, fmt.Sprintf(`input[name=songwriter_real_name_first%d]`, n), album.FirstName); err != nil {
			return "", err
		}
		if err := setValue(ctx, fmt.Sprintf(`input[name=songwriter_real_name_last%d]`, n), album.LastName); err != nil {
			return "", err
		}
		// Set song price
		if err := setMaxPrice(ctx, doc, fmt.Sprintf("#price_%s", id)); err != nil {
			return "", err
		}
		// Set instrumental
		if song.Instrumental {
			if err := click(ctx, fmt.Sprintf("#js-instrumental-radio-button-%d", n)); err != nil {
				return "", err
			}
		}
	}

	// Click on all mandatory checkboxes
	for i := 1; i <= 2; i++ {
		time.Sleep(200 * time.Millisecond)
		var checkboxes []string
		doc.Find("input[class=areyousure]").Each(func(i int, s *goquery.Selection) {
			style, ok := s.Attr("style")
			if ok && strings.Contains(strings.ReplaceAll(style, " ", ""), "display:none") {
				return
			}
			id, ok := s.Attr("id")
			if !ok {
				log.Println("distrokid: couldn't find id")
				return
			}
			checkboxes = append(checkboxes, id)
		})
		for _, id := range checkboxes {
			var isVisible bool
			checkVisibilityScript := fmt.Sprintf(`document.getElementById('%s').checkVisibility()`, id)
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(checkVisibilityScript, &isVisible),
			); err != nil {
				return "", fmt.Errorf("distrokid: couldn't check visibility of checkbox %s: %w", id, err)
			}
			if !isVisible {
				continue
			}
			if err := click(ctx, fmt.Sprintf("#%s", id)); err != nil {
				return "", err
			}
		}
	}

	// Taking a screenshot.
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
		return "", fmt.Errorf("distrokid: couldn't take screenshot: %w", err)
	}
	if _, err := os.Stat("logs"); os.IsNotExist(err) {
		if err := os.Mkdir("logs", 0755); err != nil {
			return "", fmt.Errorf("distrokid: couldn't create logs folder: %w", err)
		}
	}
	out := fmt.Sprintf("logs/%s_%s.png", time.Now().Format("20060102150405"), albumUUID)
	if err := os.WriteFile(out, buf, 0644); err != nil {
		log.Fatal(err)
	}

	if auto {
		// Click on the submit button
		if err := click(ctx, "#doneButton"); err != nil {
			return "", err
		}
	} else {
		// Wait for the user to click on the submit button manually and close the browser
		<-ctx.Done()
	}
	return albumUUID, nil
}

func getHTML(ctx context.Context, sel string) (*goquery.Document, error) {
	// Obtain the document
	var html string
	if err := chromedp.Run(ctx,
		chromedp.OuterHTML(sel, &html),
	); err != nil {
		return nil, fmt.Errorf("distrokid: couldn't get html %s: %w", sel, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("distrokid: couldn't parse doc %s: %w", sel, err)
	}
	return doc, nil
}

func selectOption(ctx context.Context, sel, val string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.SetValue(sel, val, chromedp.ByQuery),
		chromedp.Evaluate(fmt.Sprintf(`
			var event = new Event('change');
			document.querySelector('%s').dispatchEvent(event);
		`, sel), nil),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't select %s %s: %w", sel, val, err)
	}
	return nil
}

func click(ctx context.Context, sel string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.Click(sel, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't click %s: %w", sel, err)
	}
	return nil
}

func setValue(ctx context.Context, sel, val string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.SetValue(sel, val, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't set value %s %s: %w", sel, val, err)
	}
	return nil
}

func setMaxPrice(ctx context.Context, doc *goquery.Document, sel string) error {
	var maxPrice float64
	var maxPriceKey string
	doc.Find(fmt.Sprintf("%s option", sel)).Each(func(i int, s *goquery.Selection) {
		// Parse the price
		txt := s.Text()
		txt = strings.Trim(txt, "$ ")
		price, err := strconv.ParseFloat(txt, 64)
		if err != nil {
			log.Printf("couldn't parse price %s: %v\n", txt, err)
			return
		}
		key, ok := s.Attr("value")
		if !ok {
			log.Println("couldn't find value")
			return
		}
		if price > maxPrice {
			maxPrice = price
			maxPriceKey = key
		}
	})
	if maxPriceKey == "" {
		return fmt.Errorf("distrokid: couldn't find max price %s", sel)
	}
	if err := selectOption(ctx, sel, maxPriceKey); err != nil {
		return err
	}
	return nil
}

func upload(ctx context.Context, sel, file, wait string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.SetUploadFiles(sel, []string{file}),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't set upload %s %s: %w", sel, file, err)
	}
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(wait, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't wait for %s: %w", wait, err)
	}
	return nil
}

func getAlbumUUID(doc *goquery.Document) (string, error) {
	albumUUID, exists := doc.Find("#albumuuid").Attr("value")
	if !exists {
		return "", fmt.Errorf("distrokid: couldn't find albumuuid")
	}
	if albumUUID == "" {
		return "", fmt.Errorf("distrokid: albumuuid is empty")
	}
	return albumUUID, nil
}
