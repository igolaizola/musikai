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
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

type Album struct {
	Artist      string
	Title       string
	Description string
	Cover       string
	Songs       []*Song
	ReleaseDate time.Time
	UPC         string
}

type Song struct {
	Instrumental bool
	Title        string
	Description  string
	Genres       []string
	Tags         []string
	ISRC         string
	File         string
	BPM          float32
}

func (a *Album) Validate() error {
	if a.Artist == "" {
		return fmt.Errorf("jamendo: artist is empty")
	}
	if a.Title == "" {
		return fmt.Errorf("jamendo: title is empty")
	}
	if a.Description == "" {
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
		if song.ISRC == "" {
			return fmt.Errorf("jamendo: song %d ISRC is empty", i+1)
		}
		if song.BPM == 0 {
			return fmt.Errorf("jamendo: song %d BPM is empty", i+1)
		}
		if len(song.Genres) == 0 {
			return fmt.Errorf("jamendo: song %d genres is empty", i+1)
		}
		if len(song.Tags) == 0 {
			return fmt.Errorf("jamendo: song %d tags is empty", i+1)
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

	// Click on description
	if err := click(ctx, "#album_tab_menu_description"); err != nil {
		return "", err
	}
	time.Sleep(200 * time.Millisecond)

	// Set description in iframe
	var iframes []*cdp.Node
	if err := chromedp.Run(ctx, chromedp.Nodes(`iframe#LANGS_en_ifr`, &iframes, chromedp.ByQuery)); err != nil {
		return "", err
	}
	if len(iframes) == 0 {
		return "", fmt.Errorf("jamendo: couldn't find iframe")
	}
	iframe := iframes[0]
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`#tinymce p`, chromedp.ByQuery, chromedp.FromNode(iframe)),
		chromedp.Click(`#tinymce`, chromedp.ByQuery, chromedp.FromNode(iframe)),
		chromedp.SendKeys(`#tinymce`, album.Description, chromedp.ByQuery, chromedp.FromNode(iframe)),
	); err != nil {
		return "", err
	}

	time.Sleep(200 * time.Millisecond)

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

	time.Sleep(10 * time.Second)

	// Click on the album tab
	if err := click(ctx, "#albumsTab"); err != nil {
		return "", err
	}
	time.Sleep(200 * time.Millisecond)

	// Edit songs
	for i, songID := range songIDs {
		song := album.Songs[i]

		// Click on edit track
		if err := click(ctx, fmt.Sprintf(`li[data-jam-track-id="%s"] .edit button`, songID)); err != nil {
			return "", err
		}

		// Set name
		if err := setValue(ctx, "#edit_track_form #name", song.Title); err != nil {
			return "", err
		}

		// Set track number
		if err := setValue(ctx, "#client_position", strconv.Itoa(i+1)); err != nil {
			return "", err
		}

		// Set release date
		if err := setValue(ctx, "#dateReleased", album.ReleaseDate.Format("2006-01-02")); err != nil {
			return "", err
		}

		// Set no UPC code
		if err := click(ctx, `label[for="upcTrack--1"]`); err != nil {
			return "", err
		}

		// Set ISRC code
		if err := click(ctx, `label[for="isrcTrack-1"]`); err != nil {
			return "", err
		}
		if err := setValue(ctx, "#isrcCodeTrack", song.ISRC); err != nil {
			return "", err
		}
		if err := click(ctx, "#js-save-isrc-code"); err != nil {
			return "", err
		}
		time.Sleep(1000 * time.Millisecond)

		// Click on I don't have a P.R.O. association
		if err := click(ctx, `label[for="proTrack--1"]`); err != nil {
			return "", err
		}

		// Click on Lyrics
		if err := click(ctx, "#track_tab_menu_lyrics"); err != nil {
			return "", err
		}

		if song.Instrumental {
			// Click on Instrumental
			if err := click(ctx, `label[for="voice_instrumental--1"]`); err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("jamendo: only instrumental songs are supported")
		}

		if song.Description != "" {
			// Click on Description tab
			if err := click(ctx, "#track_tab_menu_description"); err != nil {
				return "", err
			}
			// Set description
			if err := setValue(ctx, "#description", song.Description); err != nil {
				return "", err
			}
		}

		// Click on tags and metadata
		if err := click(ctx, "#track_tab_menu_metadata"); err != nil {
			return "", err
		}

		// Select speed
		var speed int
		switch {
		case song.BPM <= 65:
			speed = -2
		case song.BPM <= 75:
			speed = -1
		case song.BPM <= 119:
			speed = 0
		case song.BPM <= 129:
			speed = 1
		case song.BPM > 129:
			speed = 2
		}
		if err := selectOption(ctx, "#speed", strconv.Itoa(speed)); err != nil {
			return "", err
		}

		doc, err = getHTML(ctx, "#edit_track_form")
		if err != nil {
			return "", err
		}

		// Obtain genres
		genreLookup := map[string]string{}
		doc.Find("#genres-element .option").Each(func(i int, s *goquery.Selection) {
			name := s.Text()
			v, ok := s.Attr("data-value")
			if !ok {
				log.Println("couldn't find data-value for genre", name)
				return
			}
			genreLookup[name] = v
			log.Println("genre", name, v)
		})

		// Obtain tags
		tagLookup := map[string]string{}
		doc.Find("#tags-element .option").Each(func(i int, s *goquery.Selection) {
			name := s.Text()
			v, ok := s.Attr("data-value")
			if !ok {
				log.Println("couldn't find data-value for tag", name)
				return
			}
			tagLookup[name] = v
			log.Println("tag", name, v)
		})

		// Set genres
		wait := 1000 * time.Millisecond
		for _, genre := range song.Genres {
			// Type text in #genres-selectized
			log.Println("typing genre", genre)
			if err := typeValue(ctx, "#genres-selectized", genre); err != nil {
				return "", err
			}
			time.Sleep(wait)
			if err := click(ctx, "#genres-element .option.active"); err != nil {
				return "", err
			}
			time.Sleep(wait)
			doc, err := getHTML(ctx, "select#genres")
			if err != nil {
				return "", err
			}
			var found bool
			doc.Find("option").Each(func(i int, s *goquery.Selection) {
				log.Println("option", s.Text())
				if s.Text() != genre {
					return
				}
				found = true
			})
			if !found {
				log.Printf("❌ couldn't find genre %q (%s - %s)\n", genre, album.Title, song.Title)
				// return "", fmt.Errorf("jamendo: couldn't find genre %s", genre)
			}
		}

		// Set tags
		for _, tag := range song.Tags {
			// Type text in #tags-selectized
			log.Println("typing tag", tag)
			if err := typeValue(ctx, "#tags-selectized", tag); err != nil {
				return "", err
			}
			time.Sleep(wait)
			if err := click(ctx, "#tags-element .option.active"); err != nil {
				return "", err
			}
			time.Sleep(wait)
			doc, err := getHTML(ctx, "select#tags")
			if err != nil {
				return "", err
			}
			var found bool
			doc.Find("option").Each(func(i int, s *goquery.Selection) {
				log.Println("option", s.Text())
				if s.Text() != tag {
					return
				}
				found = true
			})
			if !found {
				log.Printf("❌ couldn't find tag %q (%s - %s)\n", tag, album.Title, song.Title)
				// return "", fmt.Errorf("jamendo: couldn't find tags %s", tag)
			}
		}

		// Click on OK
		log.Println("clicking OK")
		if err := click(ctx, "#edit_track_form #submit"); err != nil {
			return "", err
		}
		time.Sleep(1000 * time.Millisecond)
		log.Println("song", song.Title, "done")
	}

	// TODO: finalize album
	fmt.Println("done, waiting for 1 minute")
	time.Sleep(1 * time.Minute)
	return albumID, nil
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

func setValue(ctx context.Context, sel, val string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.SetValue(sel, val, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("jamendo: couldn't set value %s %s: %w", sel, val, err)
	}
	return nil
}

func typeValue(ctx context.Context, sel, val string) error {
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.SendKeys(sel, val, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("jamendo: couldn't type value %s %s: %w", sel, val, err)
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
