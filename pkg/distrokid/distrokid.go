package distrokid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

	fmt.Println(js)

	var me meResponse
	if err := json.Unmarshal([]byte(js), &me); err != nil {
		return fmt.Errorf("distrokid: couldn't unmarshal me response: %w", err)
	}
	if me.ID == 0 {
		return fmt.Errorf("distrokid: couldn't find user ID")
	}
	return nil
}

type successResponse struct {
	Success bool `json:"success"`
}

func (c *Client) New(ctx context.Context) error {
	resp, err := c.do(ctx, "GET", "new/", nil, nil)
	if err != nil {
		return fmt.Errorf("distrokid: couldn't get new: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp))
	if err != nil {
		return fmt.Errorf("distrokid: couldn't parse new: %w", err)
	}
	// Search input
	// <input type="hidden" name="albumuuid" id="albumuuid" value="4A00DF27-C23E-4CE7-837EEB2B0EA95E57">
	albumUUID, exists := doc.Find("#albumuuid").Attr("value")
	if !exists {
		return fmt.Errorf("distrokid: couldn't find albumuuid")
	}
	if albumUUID == "" {
		return fmt.Errorf("distrokid: albumuuid is empty")
	}
	// Search input
	// <input type="hidden" name="CSRF" id="CSRF" value="523663f7-cb9a-40d3-9cb3-47a529a9f9d2">
	csrf, exists := doc.Find("#CSRF").Attr("value")
	if !exists {
		return fmt.Errorf("distrokid: couldn't find CSRF")
	}
	if csrf == "" {
		return fmt.Errorf("distrokid: CSRF is empty")
	}

	// Enable snapchat album
	form := url.Values{}
	form.Set("albumuuid", albumUUID)
	form.Set("action", "add")

	var okResp successResponse
	if _, err := c.do(ctx, "POST", "api/snap/snapGrant/", form, &okResp); err != nil {
		return fmt.Errorf("distrokid: couldn't enable snapchat album: %w", err)
	}
	if !okResp.Success {
		return fmt.Errorf("distrokid: couldn't enable snapchat album")
	}
	return nil

}
