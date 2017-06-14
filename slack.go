package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"errors"

	"./db"
	"./rollbar"
)

const (
	slackUnfurlURL      = "https://slack.com/api/chat.unfurl"
	slackOauthAccessURL = "https://slack.com/api/oauth.access"
)

type slackOauthAccessResponse struct {
	OK          bool
	Error       string
	AccessToken string `json:"access_token"`
	Scope       string
	UserID      string `json:"user_id"`
	TeamName    string `json:"team_name"`
	TeamID      string `json:"team_id"`
}

type slackUnfurlPayload struct {
	Token   string
	Channel string
	TS      string
	Unfurls string
}

type slackAttachment struct {
	//Text string
	Fallback string                 `json:"fallback"`
	Title    string                 `json:"title"`
	TS       int64                  `json:"ts"`
	Fields   []slackAttachmentField `json:"fields"`
}

type slackAttachmentField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

type slackOuterEvent struct {
	Token     string
	TeamID    string `json:"team_id"`
	APIAppID  string `json:"api_app_id"`
	Event     slackEvent
	Challenge string
	Type      string
	EventID   string `json:"event_id"`
}

type slackEvent struct {
	Type string
	//link_shared-specific fields
	Channel   string
	User      string
	MessageTS string `json:"message_ts"`
	Links     []struct {
		Domain string
		URL    string
	}
	//tokens_revoked-specific fields
	Tokens struct {
		OAuth []string
	}
}

func exchangeOauthCodeForToken(code string) error {
	form := url.Values{}
	form.Add("client_id", config.ClientID)
	form.Add("client_secret", config.ClientSecret)
	form.Add("code", code)

	log.Print("Posting oauth.access")

	resp, err := http.PostForm(slackOauthAccessURL, form)
	if err != nil {
		log.Printf("error when posting oauth.access: %s", err.Error())
		return err
	}
	defer resp.Body.Close()
	var oauthResponse slackOauthAccessResponse
	err = json.NewDecoder(resp.Body).Decode(&oauthResponse)
	if err != nil {
		log.Printf("error when deserializing oauth.access response: %s", err.Error())
		return err
	}
	if !oauthResponse.OK {
		log.Printf("oauth.access reported an error: %s", oauthResponse.Error)
		return errors.New(oauthResponse.Error)
	}
	err = db.SaveAuthToken(oauthResponse.TeamID, oauthResponse.UserID, oauthResponse.AccessToken)
	if err != nil {
		log.Printf("Could not save auth token: %s", err.Error())
		return err
	}
	log.Printf("Saved auth token for user %s/team %s (%s)", oauthResponse.UserID, oauthResponse.TeamID, oauthResponse.TeamName)
	return nil
}

func slackEventHandler(w http.ResponseWriter, r *http.Request) {
	event := new(slackOuterEvent)

	err := json.NewDecoder(r.Body).Decode(&event)
	if err != nil {
		log.Print("Invalid JSON received: " + err.Error())
		http.Error(w, err.Error(), 400)
		return
	}

	if event.Token != config.SlackVerificationToken {
		log.Printf("Request verification token %s did not match the one configured!", event.Token)
		http.Error(w, "Token mismatch", 403)
		return
	}

	team := event.TeamID
	log.Printf("Received event of type %s from team %s", event.Type, team)

	switch event.Type {
	case "url_verification":
		processURLVerification(w, event)
	case "event_callback":
		innerEventType := event.Event.Type
		switch innerEventType {
		case "link_shared":
			processLinkSharedEvent(&event.Event, team)
		case "tokens_revoked":
			processTokensRevokedEvent(&event.Event, team)
		case "app_uninstalled":
			processAppUninstalledEvent(team)
		default:
			log.Printf("Unsupported event subtype %s", innerEventType)
		}
	default:
		log.Printf("Unknown event type %s", event.Type)
		//ignore the event and return the default 200 OK / empty response
	}

}

func processLinkSharedEvent(e *slackEvent, team string) {
	logLine := fmt.Sprintf("link shared event (channel=%s,ts=%s), links:\n", e.Channel, e.MessageTS)
	for _, v := range e.Links {
		logLine += fmt.Sprintf("-- %s\n", v.URL)
	}
	log.Print(logLine)
	go addLinkPreviews(e, team)
}

func processTokensRevokedEvent(e *slackEvent, team string) {
	for _, v := range e.Tokens.OAuth {
		log.Printf("Deleting oAuth token for user %s (team %s) ", v, team)
		db.DeleteUserToken(team, v)
	}
}

func processAppUninstalledEvent(team string) {
	log.Printf("Deleting team %s's data", team)
	db.DeleteTeam(team)
}

func processURLVerification(w http.ResponseWriter, e *slackOuterEvent) {
	// just respond by printing the challenge back
	fmt.Fprintf(w, "%s", e.Challenge)
}

var rollbarItemRegex = regexp.MustCompile(`(\w+\/\w+)\/items\/(\d+)/?`)

func addLinkPreviews(event *slackEvent, team string) {
	linkData := make(map[string]slackAttachment)
	for _, link := range event.Links {
		url := link.URL
		matches := rollbarItemRegex.FindStringSubmatch(url)
		if len(matches) < 3 {
			log.Printf("%s is not a Rollbar item link", url)
			continue
		}
		project := strings.ToLower(matches[1])
		counter := matches[2]
		token := db.GetProjectToken(team, project)
		if token == "" {
			log.Printf("Project %s isn't configured for team %s", project, team)
			continue
		}
		item, err := rollbar.GetItemData(counter, token)
		if err != nil {
			log.Printf("error getting data for %s: %s", link.URL, err.Error())
		} else {
			linkData[link.URL] = getUnfurlData(item)
		}
	}

	if len(linkData) == 0 {
		//none of the links posted were able to be processed
		log.Printf("No links processed (channel=%s,ts=%s)", event.Channel, event.MessageTS)
		return
	}

	unfurls, err := json.Marshal(linkData)
	if err != nil {
		log.Printf("Unfurls serialization failed (channel=%s,ts=%s): %s", event.Channel, event.MessageTS, err.Error())
		return
	}

	apiToken := db.GetAuthToken(team)
	if apiToken == "" {
		log.Printf("Couldn't retrieve oAuth token for team %s", team)
		return
	}

	payload := slackUnfurlPayload{
		Token:   apiToken,
		Channel: event.Channel,
		TS:      event.MessageTS,
		Unfurls: string(unfurls),
	}

	postToSlack(payload)
}

func getTimeAgoString(d time.Duration) string {
	t := d.Seconds()
	if t < 1 {
		return "just now"
	}
	if t < 60 {
		return fmt.Sprintf("%.fs ago", t)
	}
	t /= 60 // t is now minutes
	if t < 60 {
		return fmt.Sprintf("%.fm ago", t)
	}
	t /= 60 // t is now hours
	if t < 24 {
		return fmt.Sprintf("%.fh ago", t)
	}
	t /= 24 // t is now days
	return fmt.Sprintf("%.fd ago", t)
}

func getUnfurlData(item *rollbar.Item) slackAttachment {
	now := time.Now()
	attachment := slackAttachment{
		Title:    item.Title,
		Fallback: item.Title,
		TS:       now.Unix(),
		Fields:   make([]slackAttachmentField, 4),
	}

	attachment.Fields[0] = slackAttachmentField{
		Title: "Status",
		Value: item.Status,
		Short: true,
	}
	attachment.Fields[1] = slackAttachmentField{
		Title: "Occurrences",
		Value: strconv.Itoa(item.TotalOccurrences),
		Short: true,
	}
	attachment.Fields[2] = slackAttachmentField{
		Title: "First seen",
		Value: time.Unix(int64(item.FirstOccurrenceTimestamp), 0).Format("Jan 2 15:04:05"),
		Short: true,
	}
	lastSeenAgo := now.Sub(time.Unix(int64(item.LastOccurrenceTimestamp), 0))
	attachment.Fields[3] = slackAttachmentField{
		Title: "Last seen",
		Value: getTimeAgoString(lastSeenAgo),
		Short: true,
	}

	return attachment
}

func postToSlack(message slackUnfurlPayload) {
	form := url.Values{}
	form.Add("token", message.Token)
	form.Add("channel", message.Channel)
	form.Add("ts", message.TS)
	form.Add("unfurls", message.Unfurls)

	log.Printf("Posting chat.unfurl (channel=%s,ts=%s)", message.Channel, message.TS)

	resp, err := http.PostForm(slackUnfurlURL, form)
	if err != nil {
		log.Printf("error when posting chat.unfurl: %s", err.Error())
	}
	defer resp.Body.Close()
	b, _ := ioutil.ReadAll(resp.Body)
	log.Printf("chat.unfurl resp: %s", string(b))
}
