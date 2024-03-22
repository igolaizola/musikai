package jamendo

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func (c *Client) Auth(ctx context.Context) error {
	resp, err := c.do(ctx, "GET", "", nil, nil)
	if err != nil {
		return fmt.Errorf("jamendo: couldn't get dashboard: %w", err)
	}

	// Load document from response
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp))
	if err != nil {
		return fmt.Errorf("jamendo: couldn't parse dashboard: %w", err)
	}
	href, ok := doc.Find("language-options-container li a").First().Attr("href")
	if !ok {
		return fmt.Errorf("jamendo: couldn't find language options")
	}
	if href == "" {
		return fmt.Errorf("jamendo: language options href is empty")
	}
	// Parse /fr/artist/590528/xgomusic/notifications to obtain ID an username
	split := strings.Split(href, "/")
	if len(split) < 6 {
		return fmt.Errorf("jamendo: couldn't parse language options")
	}
	id, err := strconv.Atoi(split[3])
	if err != nil {
		return fmt.Errorf("jamendo: couldn't parse ID %s: %w", split[3], err)
	}
	name := split[4]
	if name == "" {
		return fmt.Errorf("jamendo: couldn't parse username")
	}
	c.userID = id
	c.userName = name
	log.Println("jamendo: authenticated as", c.userName, "with ID", c.userID)
	return nil
}
