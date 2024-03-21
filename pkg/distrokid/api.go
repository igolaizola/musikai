package distrokid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type meResponse struct {
	Plan               string `json:"plan"`
	ArtistNamesAllowed int    `json:"artistNamesAllowed"`
	ID                 int    `json:"id"`
	Priority           int    `json:"priority"`
	UserUUID           string `json:"useruuid"`
	DisplayName        string `json:"displayName"`
	Username           string `json:"username"`
	PublicBio          string `json:"publicBio"`
	Avatar             string `json:"avatar"`
	HasPhone           bool   `json:"hasPhone"`
	Email              string `json:"email"`
}

func (c *Client) Auth(ctx context.Context) error {
	resp, err := c.do(ctx, "GET", "profile/", nil, nil)
	if err != nil {
		return fmt.Errorf("distrokid: couldn't get profile: %w", err)
	}

	// Find the first match in the HTML content
	varPattern := regexp.MustCompile(`(?s)var me\s*=\s*({.*?});`)
	matches := varPattern.FindStringSubmatch(string(resp))
	if len(matches) < 2 {
		return fmt.Errorf("distrokid: couldn't find me object")
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
		return fmt.Errorf("distrokid: couldn't unmarshal me response: %w", err)
	}
	if me.ID == 0 {
		return fmt.Errorf("distrokid: couldn't find user ID")
	}
	return nil
}

type AlbumResponse struct {
	UUID  string   `json:"uuid"`
	UPC   string   `json:"upc"`
	ISRCs []string `json:"isrcs"`
}

func (c *Client) Album(ctx context.Context, uuid string) (*AlbumResponse, error) {
	u := fmt.Sprintf("dashboard/album/?albumuuid=%s", uuid)
	resp, err := c.do(ctx, "GET", u, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("distrokid: couldn't get album: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp))
	if err != nil {
		return nil, fmt.Errorf("distrokid: couldn't parse album: %w", err)
	}

	// Search UPC
	upc := doc.Find("#js-album-upc").Text()
	if upc == "" {
		return nil, fmt.Errorf("distrokid: album UPC is empty")
	}

	// Search ISCRs
	var isrcs []string
	doc.Find(".myISRC").Each(func(i int, s *goquery.Selection) {
		isrc := s.Text()
		isrc = strings.ReplaceAll(isrc, "\n", "")
		isrc = strings.ReplaceAll(isrc, "\t", "")
		isrc = strings.Replace(isrc, "ISRC", "", 1)
		isrc = strings.TrimSpace(isrc)
		if isrc != "" {
			isrcs = append(isrcs, isrc)
		}
	})
	if len(isrcs) == 0 {
		return nil, fmt.Errorf("distrokid: couldn't find album ISRCs")
	}

	return &AlbumResponse{
		UUID:  uuid,
		UPC:   upc,
		ISRCs: isrcs,
	}, nil
}
