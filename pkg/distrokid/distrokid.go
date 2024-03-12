package distrokid

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
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
	// Write to debug file
	_ = os.WriteFile("profile.html", resp, 0644)

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
