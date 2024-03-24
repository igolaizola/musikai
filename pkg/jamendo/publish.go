package jamendo

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

type Album struct {
	Artist         string
	Title          string
	PrimaryGenre   string
	SecondaryGenre string
	Cover          string
	Songs          []*Song
	ReleaseDate    time.Time
	UPC            string
}

type Song struct {
	Instrumental bool
	Title        string
	ISRC         string
	File         string
}

func (a *Album) Validate() error {
	if a.Artist == "" {
		return fmt.Errorf("jamendo: artist is empty")
	}
	if a.Title == "" {
		return fmt.Errorf("jamendo: title is empty")
	}
	if a.PrimaryGenre == "" {
		return fmt.Errorf("jamendo: primary genre is empty")
	}
	if a.ReleaseDate.IsZero() {
		return fmt.Errorf("jamendo: release date is empty")
	}
	if len(a.Songs) == 0 {
		return fmt.Errorf("jamendo: no songs")
	}
	if a.Cover == "" {
		return fmt.Errorf("jamendo: cover is empty")
	}
	if _, err := os.Stat(a.Cover); os.IsNotExist(err) {
		return fmt.Errorf("jamendo: cover file doesn't exist: %s", a.Cover)
	}
	for i, song := range a.Songs {
		if song.Title == "" {
			return fmt.Errorf("jamendo: song %d title is empty", i+1)
		}
		if song.File == "" {
			return fmt.Errorf("jamendo: song %d file is empty", i+1)
		}
		if _, err := os.Stat(song.File); os.IsNotExist(err) {
			return fmt.Errorf("jamendo: song %d file doesn't exist: %s", i+1, song.File)
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
	u := fmt.Sprintf("https://artists.jamendo.com/en/artist/%d/%s/manager", c.artistID, c.artistName)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(u),
		chromedp.WaitVisible("body", chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("jamendo: couldn't navigate to url: %w", err)
	}

	// List existing albums
	doc, err := getHTML(ctx, "#albumsList")
	if err != nil {
		return "", err
	}
	albumLookup := map[string]struct{}{}
	doc.Find("li.album").Each(func(i int, s *goquery.Selection) {
		id, ok := s.Attr("data-jam-album-id")
		if !ok {
			return
		}
		albumLookup[id] = struct{}{}
	})

	// Click on the albums tab
	if err := click(ctx, "#albumsTab"); err != nil {
		return "", err
	}
	time.Sleep(1000 * time.Millisecond)

	// Click on the new album button
	if err := click(ctx, "#addAlbum"); err != nil {
		return "", err
	}
	time.Sleep(1000 * time.Millisecond)

	// Set the album title
	if err := setValue(ctx, "#edit_album_form #name", album.Title); err != nil {
		return "", err
	}

	// Click on OK
	if err := click(ctx, "#edit_album_form #submit"); err != nil {
		return "", err
	}

	time.Sleep(1000 * time.Millisecond)

	// List existing albums
	doc, err = getHTML(ctx, "#albumsList")
	if err != nil {
		return "", err
	}
	var albumID string
	doc.Find("li.album").Each(func(i int, s *goquery.Selection) {
		id, ok := s.Attr("data-jam-album-id")
		if !ok {
			return
		}
		if _, ok := albumLookup[id]; !ok {
			albumID = id
		}
	})
	log.Println("album id", albumID)

	// Click on the open album button
	if err := click(ctx, fmt.Sprintf(`li[data-jam-album-id="%s"] button.openAlbum`, albumID)); err != nil {
		return "", err
	}

	// Click on edit album
	if err := click(ctx, fmt.Sprintf(`li[data-jam-album-id="%s"]  button.editAlbum`, albumID)); err != nil {
		return "", err
	}

	time.Sleep(1000 * time.Millisecond)

	// Set release data
	if err := setValue(ctx, "#date_released_album", album.ReleaseDate.Format("2006-01-02")); err != nil {
		return "", err
	}

	// Set UPC code
	if err := click(ctx, `label[for="upc-1"]`); err != nil {
		return "", err
	}
	if err := setValue(ctx, "#upcCode", album.UPC); err != nil {
		return "", err
	}
	if err := click(ctx, "#js-upc-album-save-code"); err != nil {
		return "", err
	}
	time.Sleep(1000 * time.Millisecond)

	// Click on Artwork
	if err := click(ctx, "#album_tab_menu_artwork"); err != nil {
		return "", err
	}

	// Upload cover
	log.Println("uploading cover", album.Cover)
	if err := upload(ctx, `#albumArtworkFileUpload`, album.Cover, "#albumArtworkCropContainer #cropPreview"); err != nil {
		return "", err
	}
	time.Sleep(1000 * time.Millisecond)

	// Click OK
	if err := click(ctx, "#edit_album_form #submit"); err != nil {
		return "", err
	}
	if err := notVisible(ctx, "#albumTabsWrapper"); err != nil {
		return "", err
	}
	time.Sleep(1000 * time.Millisecond)

	// Click on singles
	if err := click(ctx, "#singlesTab"); err != nil {
		return "", err
	}

	// Obtain current singles
	doc, err = getHTML(ctx, "#singlesList")
	if err != nil {
		return "", err
	}
	singleLookup := map[string]struct{}{}
	doc.Find("li.track").Each(func(i int, s *goquery.Selection) {
		id, ok := s.Attr("data-jam-track-id")
		if !ok {
			return
		}
		singleLookup[id] = struct{}{}
	})

	var songIDs []string

	for _, song := range album.Songs {
		// Upload song
		name := filepath.Base(song.File)
		name = strings.TrimSuffix(name, filepath.Ext(name))
		if err := upload(ctx, `#trackFileUpload`, song.File, fmt.Sprintf(`div[title="%s"] button.play`, name)); err != nil {
			return "", err
		}

		// Obtain song ID
		var songID string
		for {
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("jamendo: context done while waiting for song id")
			default:
			}
			doc, err = getHTML(ctx, "#singlesList")
			if err != nil {
				return "", err
			}
			doc.Find("li.track").Each(func(i int, s *goquery.Selection) {
				id, ok := s.Attr("data-jam-track-id")
				if !ok {
					return
				}
				if _, ok := singleLookup[id]; !ok {
					songID = id
					singleLookup[id] = struct{}{}
				}
			})
			if songID == "" {
				time.Sleep(1 * time.Second)
				continue
			}
			break
		}
		if songID == "" {
			return "", fmt.Errorf("jamendo: couldn't find song id")
		}
		log.Println("song id", songID)
		songIDs = append(songIDs, songID)

		// TODO: Edit song data here

		// Click on select
		if err := click(ctx, fmt.Sprintf(`li[data-jam-track-id="%s"] input.js-batch-actions`, songID)); err != nil {
			return "", err
		}
	}

	// Click on batch move
	if err := click(ctx, "button.batch_move"); err != nil {
		return "", err
	}

	// Choose album
	if err := selectOption(ctx, "#move_track_form select#albumId", albumID); err != nil {
		return "", err
	}
	time.Sleep(200 * time.Millisecond)

	// Click MOVE
	if err := click(ctx, `#move_track_form input[value="move"]`); err != nil {
		return "", err
	}

	// Click on the album tab
	if err := click(ctx, "#albumsTab"); err != nil {
		return "", err
	}

	// TODO: finalize album

	<-ctx.Done()
	return "", nil
}

func getHTML(ctx context.Context, sel string) (*goquery.Document, error) {
	// Obtain the document
	var html string
	if err := chromedp.Run(ctx,
		chromedp.OuterHTML(sel, &html),
	); err != nil {
		return nil, fmt.Errorf("jamendo: couldn't get html %s: %w", sel, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("jamendo: couldn't parse doc %s: %w", sel, err)
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
		return fmt.Errorf("jamendo: couldn't select %s %s: %w", sel, val, err)
	}
	return nil
}

func click(ctx context.Context, sel string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.Click(sel, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("jamendo: couldn't click %s: %w", sel, err)
	}
	return nil
}

func notVisible(ctx context.Context, sel string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitNotVisible(sel, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("jamendo: %s is still present: %w", sel, err)
	}
	return nil
}

func clickParent(ctx context.Context, child string) error {
	script := fmt.Sprintf(
		`document.querySelector('%s').parentNode.click();`,
		child)

	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(child),
		chromedp.Evaluate(script, nil),
	); err != nil {
		return fmt.Errorf("jamendo: couldn't click parent of %s: %w", child, err)
	}
	return nil
}

func clickCheck(ctx context.Context, sel string, visible bool) error {
	if visible {
		var isVisible bool
		checkVisibilityScript := fmt.Sprintf(`document.querySelector('%s').checkVisibility()`, sel)
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(checkVisibilityScript, &isVisible),
		); err != nil {
			return fmt.Errorf("jamendo: couldn't check visibility of checkbox %s: %w", sel, err)
		}
		if !isVisible {
			return nil
		}
	}
	if err := click(ctx, sel); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	var isChecked bool
	err := chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector('%s').checked`, sel), &isChecked),
	)
	if err != nil {
		return fmt.Errorf("jamendo: couldn't check if checkbox %s is checked: %w", sel, err)
	}
	if !isChecked {
		return fmt.Errorf("jamendo: checkbox %s is not checked", sel)
	}
	return nil
}

func clickCheckParent(ctx context.Context, sel string) error {
	if err := clickParent(ctx, sel); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	var isChecked bool
	err := chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector('%s').checked`, sel), &isChecked),
	)
	if err != nil {
		return fmt.Errorf("jamendo: couldn't check if checkbox %s is checked: %w", sel, err)
	}
	if !isChecked {
		return fmt.Errorf("jamendo: checkbox %s is not checked", sel)
	}
	return nil
}

func setValue(ctx context.Context, sel, val string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.SetValue(sel, val, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("jamendo: couldn't set value %s %s: %w", sel, val, err)
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
		return fmt.Errorf("jamendo: couldn't find max price %s", sel)
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
		return fmt.Errorf("jamendo: couldn't set upload %s %s: %w", sel, file, err)
	}
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(wait, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("jamendo: couldn't wait for %s: %w", wait, err)
	}
	return nil
}

func getAlbumUUID(doc *goquery.Document) (string, error) {
	albumUUID, exists := doc.Find("#albumuuid").Attr("value")
	if !exists {
		return "", fmt.Errorf("jamendo: couldn't find albumuuid")
	}
	if albumUUID == "" {
		return "", fmt.Errorf("jamendo: albumuuid is empty")
	}
	return albumUUID, nil
}
