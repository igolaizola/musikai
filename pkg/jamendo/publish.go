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
	Energy       float32
	Mood         float32
	Acousticness float32
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
		for _, v := range song.Genres {
			if _, ok := genreValues[v]; !ok {
				return fmt.Errorf("jamendo: song %d genre %q is invalid", i+1, v)
			}
		}
		if len(song.Tags) == 0 {
			return fmt.Errorf("jamendo: song %d tags is empty", i+1)
		}
		for _, v := range song.Tags {
			if _, ok := tagValues[v]; !ok {
				return fmt.Errorf("jamendo: song %d tag %q is invalid", i+1, v)
			}
		}
		if song.Acousticness == 0 {
			return fmt.Errorf("jamendo: song %d acousticness is empty", i+1)
		}
		if song.Mood == 0 {
			return fmt.Errorf("jamendo: song %d mood is empty", i+1)
		}
		if song.Energy == 0 {
			return fmt.Errorf("jamendo: song %d energy is empty", i+1)
		}
		if song.Description == "" {
			return fmt.Errorf("jamendo: song %d description is empty", i+1)
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

type Publication struct {
	AlbumID string
	SongIDs []string
}

// Publish publishes a new album
func (c *Browser) Publish(parent context.Context, album *Album, editTracks bool) (*Publication, error) {
	// Validate album
	if err := album.Validate(); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("jamendo: couldn't navigate to url: %w", err)
	}

	// List existing albums
	doc, err := getHTML(ctx, "#albumsList")
	if err != nil {
		return nil, err
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
		return nil, err
	}
	time.Sleep(1000 * time.Millisecond)

	// Click on the new album button
	if err := click(ctx, "#addAlbum"); err != nil {
		return nil, err
	}
	time.Sleep(1000 * time.Millisecond)

	// Set the album title
	if err := setValue(ctx, "#edit_album_form #name", album.Title); err != nil {
		return nil, err
	}

	// Click on OK
	if err := click(ctx, "#edit_album_form #submit"); err != nil {
		return nil, err
	}

	time.Sleep(1000 * time.Millisecond)

	// List existing albums
	doc, err = getHTML(ctx, "#albumsList")
	if err != nil {
		return nil, err
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
		return nil, err
	}

	// Click on edit album
	if err := click(ctx, fmt.Sprintf(`li[data-jam-album-id="%s"]  button.editAlbum`, albumID)); err != nil {
		return nil, err
	}

	time.Sleep(1000 * time.Millisecond)

	// Set release data
	if err := setValue(ctx, "#date_released_album", album.ReleaseDate.Format("2006-01-02")); err != nil {
		return nil, err
	}

	// Set UPC code
	if err := click(ctx, `label[for="upc-1"]`); err != nil {
		return nil, err
	}
	if err := setValue(ctx, "#upcCode", album.UPC); err != nil {
		return nil, err
	}
	if err := click(ctx, "#js-upc-album-save-code"); err != nil {
		return nil, err
	}
	time.Sleep(1000 * time.Millisecond)

	// Click on description
	if err := click(ctx, "#album_tab_menu_description"); err != nil {
		return nil, err
	}
	time.Sleep(200 * time.Millisecond)

	// Set description in iframe
	var iframes []*cdp.Node
	if err := chromedp.Run(ctx, chromedp.Nodes(`iframe#LANGS_en_ifr`, &iframes, chromedp.ByQuery)); err != nil {
		return nil, err
	}
	if len(iframes) == 0 {
		return nil, fmt.Errorf("jamendo: couldn't find iframe")
	}
	iframe := iframes[0]
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`#tinymce p`, chromedp.ByQuery, chromedp.FromNode(iframe)),
		chromedp.Click(`#tinymce`, chromedp.ByQuery, chromedp.FromNode(iframe)),
		chromedp.SendKeys(`#tinymce`, album.Description, chromedp.ByQuery, chromedp.FromNode(iframe)),
	); err != nil {
		return nil, err
	}

	time.Sleep(200 * time.Millisecond)

	// Click on Artwork
	if err := click(ctx, "#album_tab_menu_artwork"); err != nil {
		return nil, err
	}

	// Upload cover
	log.Println("uploading cover", album.Cover)
	if err := upload(ctx, `#albumArtworkFileUpload`, album.Cover, "#albumArtworkCropContainer #cropPreview"); err != nil {
		return nil, err
	}
	time.Sleep(1000 * time.Millisecond)

	// Click OK
	if err := click(ctx, "#edit_album_form #submit"); err != nil {
		return nil, err
	}
	if err := notVisible(ctx, "#albumTabsWrapper"); err != nil {
		return nil, err
	}
	time.Sleep(1000 * time.Millisecond)

	// Click on singles
	if err := click(ctx, "#singlesTab"); err != nil {
		return nil, err
	}

	// Obtain current singles
	doc, err = getHTML(ctx, "#singlesList")
	if err != nil {
		return nil, err
	}
	singleLookup := map[string]struct{}{}
	doc.Find("li.track").Each(func(i int, s *goquery.Selection) {
		id, ok := s.Attr("data-jam-track-id")
		if !ok {
			return
		}
		singleLookup[id] = struct{}{}
	})

	songIDs := make([]string, len(album.Songs))
	for i := len(album.Songs) - 1; i >= 0; i-- {
		song := album.Songs[i]

		// Upload song
		name := filepath.Base(song.File)
		name = strings.TrimSuffix(name, filepath.Ext(name))
		waitSel := fmt.Sprintf(`li[data-jam-track-status="uploaderror"] div[title="%s"], li[data-jam-track-status="uploaded"] div[title="%s"]`, name, name)
		if err := upload(ctx, `#trackFileUpload`, song.File, waitSel); err != nil {
			return nil, err
		}

		// Obtain song ID
		var songID string
		for {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("jamendo: context done while waiting for song id")
			default:
			}
			doc, err = getHTML(ctx, "#singlesList")
			if err != nil {
				return nil, err
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
			return nil, fmt.Errorf("jamendo: couldn't find song id")
		}
		// Obtain data-jam-track-status
		var status string
		err := chromedp.Run(ctx,
			chromedp.WaitVisible(`li.track[data-jam-track-id="`+songID+`"]`),
			// Retrieve the 'data-jam-track-status' attribute from the element
			chromedp.AttributeValue(`li.track[data-jam-track-id="`+songID+`"]`, "data-jam-track-status", &status, nil),
		)
		if err != nil {
			return nil, fmt.Errorf("jamendo: couldn't get song status: %w", err)
		}
		if status == "uploaderror" {
			log.Println("❌ upload error, deleting and retrying...")
			// Click on delete
			if err := click(ctx, fmt.Sprintf(`li[data-jam-track-id="%s"] div.delete`, songID)); err != nil {
				return nil, err
			}
			i--
			continue
		}

		log.Println("song id", songID)
		songIDs[i] = songID

		// Click on select
		if err := click(ctx, fmt.Sprintf(`li[data-jam-track-id="%s"] input.js-batch-actions`, songID)); err != nil {
			return nil, err
		}
	}

	if editTracks {
		// Click on batch move
		if err := click(ctx, "button.batch_move"); err != nil {
			return nil, err
		}
		// Choose album
		if err := selectOption(ctx, "#move_track_form select#albumId", albumID); err != nil {
			return nil, err
		}
		time.Sleep(200 * time.Millisecond)
		// Click MOVE
		if err := click(ctx, `#move_track_form input[value="move"]`); err != nil {
			return nil, err
		}
		// Wait
		wait := time.Duration(len(album.Songs)*1500) * time.Millisecond
		if wait < 5*time.Second {
			wait = 5 * time.Second
		}
		time.Sleep(wait)

		// Move missing ones

		// Obtain current singles
		doc, err = getHTML(ctx, "#singlesList")
		if err != nil {
			return nil, err
		}
		missingLookup := map[string]struct{}{}
		doc.Find("li.track").Each(func(i int, s *goquery.Selection) {
			id, ok := s.Attr("data-jam-track-id")
			if !ok {
				return
			}
			missingLookup[id] = struct{}{}
		})

		var missing int
		for _, songID := range songIDs {
			if _, ok := missingLookup[songID]; !ok {
				continue
			}
			// Click on select
			if err := click(ctx, fmt.Sprintf(`li[data-jam-track-id="%s"] input.js-batch-actions`, songID)); err != nil {
				return nil, err
			}
			missing++
		}

		if missing > 0 {
			// Click on batch move
			if err := click(ctx, "button.batch_move"); err != nil {
				return nil, err
			}
			// Choose album
			if err := selectOption(ctx, "#move_track_form select#albumId", albumID); err != nil {
				return nil, err
			}
			time.Sleep(200 * time.Millisecond)
			// Click MOVE
			if err := click(ctx, `#move_track_form input[value="move"]`); err != nil {
				return nil, err
			}
			// Wait
			wait := time.Duration(missing*1500) * time.Millisecond
			if wait < 5*time.Second {
				wait = 5 * time.Second
			}
			time.Sleep(wait)
		}

		if err := c.EditTracks(ctx, album, albumID, songIDs); err != nil {
			return nil, err
		}
	}
	time.Sleep(5 * time.Second)

	return &Publication{
		AlbumID: albumID,
		SongIDs: songIDs,
	}, nil
}

func (c *Browser) EditTracks(ctx context.Context, album *Album, albumID string, songIDs []string) error {
	// Refresh the page
	u := fmt.Sprintf("https://artists.jamendo.com/en/artist/%d/%s/manager", c.artistID, c.artistName)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(u),
		chromedp.WaitVisible("body", chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("jamendo: couldn't navigate to url: %w", err)
	}

	// Click on the album tab
	if err := click(ctx, "#albumsTab"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	// Click on the open album button
	if err := click(ctx, fmt.Sprintf(`li[data-jam-album-id="%s"] button.openAlbum`, albumID)); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	// Edit songs
	for i, songID := range songIDs {
		song := album.Songs[i]

		// Click on edit track
		if err := click(ctx, fmt.Sprintf(`li[data-jam-track-id="%s"] .edit button`, songID)); err != nil {
			return err
		}

		// Set name
		if err := setValue(ctx, "#edit_track_form #name", song.Title); err != nil {
			return err
		}

		// Set track number
		if err := setValue(ctx, "#client_position", strconv.Itoa(i+1)); err != nil {
			return err
		}

		// Set release date
		if err := setValue(ctx, "#dateReleased", album.ReleaseDate.Format("2006-01-02")); err != nil {
			return err
		}

		// Set no UPC code
		// This is only for singles
		/*
			if err := click(ctx, `label[for="upcTrack--1"]`); err != nil {
				return err
			}
		*/

		// Set ISRC code
		if err := click(ctx, `label[for="isrcTrack-1"]`); err != nil {
			return err
		}
		if err := setValue(ctx, "#isrcCodeTrack", song.ISRC); err != nil {
			return err
		}
		if err := click(ctx, "#js-save-isrc-code"); err != nil {
			return err
		}
		time.Sleep(1000 * time.Millisecond)

		// Click on I don't have a P.R.O. association
		if err := click(ctx, `label[for="proTrack--1"]`); err != nil {
			return err
		}

		// Click on Lyrics
		if err := click(ctx, "#track_tab_menu_lyrics"); err != nil {
			return err
		}

		if song.Instrumental {
			// Click on Instrumental
			if err := click(ctx, `label[for="voice_instrumental--1"]`); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("jamendo: only instrumental songs are supported")
		}

		if song.Description != "" {
			// Click on Description tab
			if err := click(ctx, "#track_tab_menu_description"); err != nil {
				return err
			}
			// Set description
			if err := setValue(ctx, "#description", song.Description); err != nil {
				return err
			}
		}

		// Click on tags and metadata
		if err := click(ctx, "#track_tab_menu_metadata"); err != nil {
			return err
		}

		// Select speed
		speed := toSpeed(song.BPM)
		if err := selectOption(ctx, "#speed", strconv.Itoa(speed)); err != nil {
			return err
		}

		// Select energy
		if song.Energy > 0.0 {
			energy := toLevel(song.Energy)
			if err := selectOption(ctx, "#energy", strconv.Itoa(energy)); err != nil {
				return err
			}
		}

		// Select mood
		if song.Mood > 0.0 {
			mood := toLevel(song.Mood)
			if err := selectOption(ctx, "#happy_sad", strconv.Itoa(mood)); err != nil {
				return err
			}
		}

		// Select acoustic or electric
		if song.Acousticness < 0.4 {
			// Click on electric
			if err := click(ctx, `label[for="acoustic_electric--1"]`); err != nil {
				return err
			}
		} else if song.Acousticness > 0.6 {
			// Click on acoustic
			if err := click(ctx, `label[for="acoustic_electric-1"]`); err != nil {
				return err
			}
		}

		doc, err := getHTML(ctx, "#edit_track_form")
		if err != nil {
			return err
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
				return err
			}
			time.Sleep(wait)
			if err := click(ctx, "#genres-element .option.active"); err != nil {
				return err
			}
			time.Sleep(wait)
			doc, err := getHTML(ctx, "select#genres")
			if err != nil {
				return err
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
				// return fmt.Errorf("jamendo: couldn't find genre %s", genre)
			}
		}

		// Set tags
		for _, tag := range song.Tags {
			// Type text in #tags-selectized
			log.Println("typing tag", tag)
			if err := typeValue(ctx, "#tags-selectized", tag); err != nil {
				return err
			}
			time.Sleep(wait)
			if err := click(ctx, "#tags-element .option.active"); err != nil {
				return err
			}
			time.Sleep(wait)
			doc, err := getHTML(ctx, "select#tags")
			if err != nil {
				return err
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
				// return fmt.Errorf("jamendo: couldn't find tags %s", tag)
			}
		}

		// Click on OK
		log.Println("clicking OK")
		if err := click(ctx, "#edit_track_form #submit"); err != nil {
			return err
		}
		time.Sleep(2000 * time.Millisecond)
		log.Println("song", song.Title, "done")
	}
	return nil
}

func toLevel(f float32) int {
	switch {
	case f <= 0.2:
		return -2
	case f <= 0.4:
		return -1
	case f <= 0.6:
		return 0
	case f <= 0.8:
		return 1
	default:
		return 2
	}
}

func toSpeed(bpm float32) int {
	switch {
	case bpm <= 65:
		return -2
	case bpm <= 75:
		return -1
	case bpm <= 119:
		return 0
	case bpm <= 129:
		return 1
	default:
		return 2
	}
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
