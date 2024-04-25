package jamendo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//{"uploadserverImg":"https:\/\/usercontent.jamendo.com?type=artist&id=590528&width=300&t=1711232118","responseStatus":"success","albums":[]}

type albumsResponse struct {
	UploadServerImg string `json:"uploadserverImg"`
	ResponseStatus  string `json:"responseStatus"`
	Albums          []struct{}
}

func (c *Client) Auth(ctx context.Context) error {
	var resp albumsResponse
	if _, err := c.do(ctx, "GET", fmt.Sprintf("trackmanager/albums/%d/%s/json", c.id, c.name), nil, &resp); err != nil {
		return fmt.Errorf("jamendo: couldn't get dashboard: %w", err)
	}

	// Check if the response is successful
	if resp.ResponseStatus != "success" {
		return fmt.Errorf("jamendo: couldn't get albums: %s", resp.ResponseStatus)
	}
	return nil
}

type ticketResponse struct {
	UploadServerImg string `json:"uploadserverImg"`
	Datas           struct {
		TicketData      string `json:"ticket_data"`
		TicketSignature string `json:"ticket_signature"`
	} `json:"datas"`
}

type trackResponse struct {
	UploadServerImg string `json:"uploadserverImg"`
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Filename        string `json:"filename"`
	Albums          []struct {
		TrackNo int `json:"trackNo"`
	} `json:"albums"`
	Moderation        string `json:"moderation"`
	JamloadUserID     int    `json:"jamload_userid"`
	Types             string `json:"types"`
	LicenseID         int    `json:"license_id"`
	ArtistID          string `json:"artistId"`
	Status            string `json:"status"`
	DateJamloaderinit string `json:"date_jamloaderinit"`
}

type trackRequest struct {
	IsSingle                       bool                   `json:"isSingle"`
	Name                           string                 `json:"name"`
	Filename                       string                 `json:"filename"`
	Status                         string                 `json:"status"`
	StatusData                     string                 `json:"status_data"`
	DateCreated                    string                 `json:"date_created"`
	Credits                        string                 `json:"credits"`
	Description                    *string                `json:"description"` // Null or a string
	AlbumId                        *int                   `json:"albumId"`     // Null or an integer
	ClientPosition                 int                    `json:"client_position"`
	ClientSize                     int64                  `json:"client_size"`
	ClientType                     string                 `json:"client_type"`
	ClientStatus                   string                 `json:"client_status"`
	ArtistID                       int                    `json:"artist_id"`
	ArtistName                     string                 `json:"artist_name"`
	ClientValidationErrors         []interface{}          `json:"client_validation_errors"` // Assuming no specific structure
	Hash                           string                 `json:"hash"`
	AlbumHash                      string                 `json:"album_hash"`
	Moderation                     string                 `json:"moderation"`
	CoverUpload                    int                    `json:"cover_upload"`
	TrackNo                        int                    `json:"track_no"`
	DateJamloaderInit              string                 `json:"date_jamloaderinit"`
	JamloadUserid                  string                 `json:"jamload_userid"`
	OnProCut                       string                 `json:"on_pro_cut"`
	OnProFlow                      string                 `json:"on_pro_flow"`
	LicenseId                      int                    `json:"license_id"`
	LicenseJurisdication           string                 `json:"license_jurisdication"`
	AllowCommercial                string                 `json:"allow_commercial"`
	AllowModifications             string                 `json:"allow_modifications"`
	Size                           int64                  `json:"size"`
	Duration                       int                    `json:"duration"`
	Type                           string                 `json:"type"`
	UploadUrl                      string                 `json:"upload_url"`
	JamloadLocalname               string                 `json:"jamload_localname"`
	JamloadServer                  string                 `json:"jamload_server"`
	DateReleased                   string                 `json:"dateReleased"`
	VoiceInstrumental              string                 `json:"voice_instrumental"`
	ExplicitLyrics                 int                    `json:"explicit_lyrics"`
	LyricsLanguage                 int                    `json:"lyrics_language"`
	LyricsText                     string                 `json:"lyrics_text"`
	MaleFemale                     string                 `json:"male_female"`
	Tags                           map[string]interface{} `json:"tags"` // Assuming arbitrary key-value pairs
	Energy                         string                 `json:"energy"`
	Speed                          string                 `json:"speed"`
	HappySad                       string                 `json:"happy_sad"`
	AcousticElectric               string                 `json:"acoustic_electric"`
	AdrevContractStatus            string                 `json:"adrevContractStatus"`
	AdrevContractDateAdded         string                 `json:"adrevContractDateAdded"`
	AdrevContractDateStart         string                 `json:"adrevContractDateStart"`
	AdrevContractDateEnd           string                 `json:"adrevContractDateEnd"`
	AdrevContractDateAskInActivate string                 `json:"adrevContractDateAskInActivate"`
	AdrevIsLocked                  string                 `json:"adrevIsLocked"`
	AdrevActiveByDefault           string                 `json:"adrevActiveByDefault"` // "null" string or actual value
	RelativePath                   string                 `json:"relativePath"`
	LastModified                   int64                  `json:"lastModified"`
	LastModifiedDate               string                 `json:"lastModifiedDate"`
	WebkitRelativePath             string                 `json:"webkitRelativePath"`
}

func (c *Client) Upload(ctx context.Context, path string) error {
	filename := filepath.Base(path)
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	trackReq := &trackRequest{
		IsSingle:             true,
		Name:                 name,
		Filename:             filename,
		Status:               "uploading",
		ClientPosition:       1,
		ClientSize:           30168842,
		ClientType:           "audio/wav",
		ArtistID:             c.id,
		ArtistName:           c.name,
		Hash:                 "e14n8br3l3n1711233391938",
		AlbumHash:            "this_is_the_singles_hash",
		OnProCut:             "no",
		OnProFlow:            "no",
		LicenseId:            86,
		LicenseJurisdication: "int",
		AllowCommercial:      "n",
		AllowModifications:   "n",
		Size:                 30168842,
		Type:                 "audio/wav",
		LastModified:         1711233380156,
		LastModifiedDate:     time.Unix(0, 1711233380156*int64(time.Millisecond)).Format("2024-03-23T22:36:20.156Z"),
	}
	var trackResp trackResponse
	if _, err := c.do(ctx, "POST", fmt.Sprintf("trackmanager/tracks/%d/%s/json", c.id, c.name), trackReq, &trackResp); err != nil {
		return fmt.Errorf("jamendo: couldn't set track: %w", err)
	}

	// Get ticket
	u := fmt.Sprintf("artist/%d/%s/manager/getticket?format=json", c.id, c.name)
	var ticket ticketResponse
	if _, err := c.do(ctx, "GET", u, nil, &ticket); err != nil {
		return fmt.Errorf("jamendo: couldn't get ticket: %w", err)
	}

	// TODO: Upload in chunks

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.SetBoundary(fmt.Sprintf("----WebKitFormBoundary%s", webkitID(16))); err != nil {
		return fmt.Errorf("jamendo: couldn't set boundary: %w", err)
	}

	// Add fields
	kvs := []struct {
		key   string
		value string
	}{
		{key: "user_id", value: fmt.Sprintf("%d", 0)}, // TODO: obtain user ID
		{key: "artist_id", value: fmt.Sprintf("%d", c.id)},
		{key: "secret", value: ""},                    // TODO: obtain secret
		{key: "file_id", value: fmt.Sprintf("%d", 0)}, // TODO: obtain file ID
		{key: "ticket_data", value: ticket.Datas.TicketData},
		{key: "ticket_signature", value: ticket.Datas.TicketSignature},
	}
	for _, kv := range kvs {
		if err := writer.WriteField(kv.key, kv.value); err != nil {
			return fmt.Errorf("jamendo: couldn't write field %s: %w", kv.key, err)
		}
	}

	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return fmt.Errorf("jamendo: couldn't create form file: %w", err)
	}

	// Open file
	reader, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("jamendo: couldn't open file: %w", err)
	}
	defer reader.Close()

	// Copy file to part
	if _, err := io.Copy(part, reader); err != nil {
		return fmt.Errorf("jamendo: couldn't copy file to part: %w", err)
	}

	// Close writer
	if err := writer.Close(); err != nil {
		return fmt.Errorf("jamendo: couldn't close writer: %w", err)
	}

	// Upload file
	f := &form{
		writer: writer,
		data:   &buf,
	}
	uploadURL := "https://uploadserver.jamendo.com/audio/index.php"
	if _, err := c.do(ctx, "POST", uploadURL, f, nil); err != nil {
		return err
	}
	return nil
}

type updateTrackRequest struct {
	AlbumIDPlaceholder string    `json:"albumId-placeholder"`
	IsrcTrack          string    `json:"isrcTrack"`
	IsrcCodeTrack      string    `json:"isrcCodeTrack"`
	UpcTrack           *string   `json:"upcTrack"`
	UpcCodeTrack       string    `json:"upcCodeTrack"`
	ProTrack           string    `json:"proTrack"`
	ProCodeTrack       string    `json:"proCodeTrack"`
	AcousticElectric   string    `json:"acoustic_electric"`
	Speed              string    `json:"speed"`
	Energy             string    `json:"energy"`
	HappySad           string    `json:"happy_sad"`
	Tags               trackTags `json:"tags"`
}

type trackTags struct {
	Tags   []valueLabel `json:"tags"`
	Genres []valueLabel `json:"genres"`
}

type valueLabel struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type updateTrackResponse struct {
	ID int `json:"id"`
}

func (c *Client) UpdateTrack(ctx context.Context, album string, song *Song, id string) error {
	var tTags trackTags
	for _, label := range song.Tags {
		value, ok := tagValues[label]
		if !ok {
			return fmt.Errorf("jamendo: couldn't update track: invalid tag %s", label)
		}
		tTags.Tags = append(tTags.Tags, valueLabel{Value: value, Label: label})
	}
	for _, label := range song.Genres {
		value, ok := genreValues[label]
		if !ok {
			return fmt.Errorf("jamendo: couldn't update track: invalid genre %s", label)
		}
		tTags.Genres = append(tTags.Genres, valueLabel{Value: value, Label: label})
	}

	// Select speed
	speed := strconv.Itoa(toSpeed(song.BPM))

	// Select energy
	var energy string
	if song.Energy > 0.0 {
		energy = strconv.Itoa(toLevel(song.Energy))
	}

	// Select mood
	var mood string
	if song.Mood > 0.0 {
		mood = strconv.Itoa(toLevel(song.Mood))
	}

	// Select acoustic or electric
	var acousticElectric string
	if song.Acousticness < 0.4 {
		acousticElectric = "-1"
	} else if song.Acousticness > 0.6 {
		acousticElectric = "1"
	}

	req := &updateTrackRequest{
		AlbumIDPlaceholder: album,
		IsrcTrack:          "1",
		IsrcCodeTrack:      song.ISRC,
		UpcTrack:           nil,
		UpcCodeTrack:       "",
		ProTrack:           "-1",
		ProCodeTrack:       "",
		AcousticElectric:   acousticElectric,
		Speed:              speed,
		Energy:             energy,
		HappySad:           mood,
	}
	var resp updateTrackResponse
	if _, err := c.do(ctx, "PATCH", fmt.Sprintf("trackmanager/tracks/%d/%s/json/%s", c.id, c.name, id), req, &resp); err != nil {
		return fmt.Errorf("jamendo: couldn't update track: %w", err)
	}
	if id != strconv.Itoa(resp.ID) {
		return fmt.Errorf("jamendo: couldn't update track: invalid ID")
	}
	return nil
}

func (c *Client) UpdateTracks(ctx context.Context, album string, songs []*Song, ids []string) error {
	for i, song := range songs {
		if err := c.UpdateTrack(ctx, album, song, ids[i]); err != nil {
			return err
		}
	}
	return nil
}
