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
	maxStacktraceFrames = 10
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
	MrkdwnIn string                 `json:"mrkdwn_in"`
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

func oauthCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	if code != "" {
		err := exchangeOauthCodeForToken(code)
		if err != nil {
			// serve error page?
		}
	}
	serveFile(w, "static/thanks.html")
}

func slashCommandHandler(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	if token != config.SlackVerificationToken {
		log.Printf("Token %s did not match the configured one at /slash", token)
		http.Error(w, "Token mismatch", 403)
		return
	}
	team := r.FormValue("team_id")
	user := r.FormValue("user_id")
	command := r.FormValue("command")
	text := r.FormValue("text")
	log.Printf("Received slash command (team %s, user %s): %s %s", team, user, command, text)
	switch command {
	case "/rollbar":
		processRollbarSlashCommand(w, text, team)
	default:
		log.Printf("Unsupported slack command %s", command)
	}
}

type slackSlashCommandResponse struct {
	ResponseType string `json:"response_type"`
	Text         string `json:"text"`
}

const (
	rollbarCmdUsage = "Usage:\n" +
		"`/rollbar set <project url> <project token>` - set read access token for project\n" +
		"`/rollbar clear <project url>` - clear access token for project\n" +
		"`/rollbar list` - list all projects that I will unfurl\n\n" +
		"For example: `/rollbar set https://rollbar.com/MyOrganization/MyProject/ abcdef12345`"
	rollbarInvalidProjectURL = "Sorry, %s doesn't look like a Rollbar project URL. It should look like this: " +
		"https://rollbar.com/MyOrganization/MyProject/"
	rollbarInvalidToken = "Sorry, Rollbar reports %s is not a valid access token. Please copy the _read_ token from " +
		"https://rollbar.com/%s/settings/access_tokens/"
	rollbarTokenAdded           = "Thanks! I will now unfurl links from https://rollbar.com/%s/items/ for you."
	rollbarTokenRemoved         = "Done! I will no longer unfurl links from https://rollbar.com/%s/items/."
	rollbarGeneralError         = "An error occurred while executing the command. Please try again!"
	rollbarNoProjectsConfigured = "No  Rollbar projects have been configured for your team.\n" +
		"Use `/rollbar set` to add one."
	rollbarProjectList = "I will unfurl links from the following projects:\n%s"
)

func processRollbarSlashCommand(w http.ResponseWriter, commandText, team string) {
	resp := slackSlashCommandResponse{
		ResponseType: "ephemeral",
	}
	parts := strings.Split(commandText, " ")
	switch parts[0] {
	case "list":
		projects := db.GetProjects(team)
		for k, p := range projects {
			projects[k] = fmt.Sprintf("https://rollbar.com/%s/", p)
		}
		if len(projects) == 0 {
			resp.Text = rollbarNoProjectsConfigured
			break
		}
		resp.Text = fmt.Sprintf(rollbarProjectList, strings.Join(projects, "\n"))
	case "set":
		if len(parts) != 3 {
			resp.Text = rollbarCmdUsage
			break
		}
		projectURL := parts[1]
		matches := rollbarProjectRegex.FindStringSubmatch(projectURL)
		if len(matches) != 3 {
			resp.Text = fmt.Sprintf(rollbarInvalidProjectURL, projectURL)
			break
		}
		project := strings.ToLower(matches[1])
		token := parts[2]
		if !rollbar.IsValidToken(token) {
			resp.Text = fmt.Sprintf(rollbarInvalidToken, token, project)
			break
		}
		//finally, all is well
		err := db.SaveProjectToken(team, project, token)
		if err != nil {
			resp.Text = rollbarGeneralError
			break
		}
		resp.Text = fmt.Sprintf(rollbarTokenAdded, project)
	case "clear":
		if len(parts) != 2 {
			resp.Text = rollbarCmdUsage
			break
		}
		projectURL := parts[1]
		matches := rollbarProjectRegex.FindStringSubmatch(projectURL)
		if len(matches) != 3 {
			resp.Text = fmt.Sprintf(rollbarInvalidProjectURL, projectURL)
			break
		}
		project := strings.ToLower(matches[1])
		db.DeleteProjectToken(team, project)
		resp.Text = fmt.Sprintf(rollbarTokenRemoved, project)
	default:
		resp.Text = rollbarCmdUsage
	}

	b, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
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
	log.Printf("Received event of type %s/%s from team %s", event.Type, event.Event.Type, team)

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

var rollbarItemRegex = regexp.MustCompile(`([a-zA-Z0-9_\-\.]+\/[a-zA-Z0-9_\-\.]+)\/items\/(\d+)/?`)
var rollbarProjectRegex = regexp.MustCompile(`https?:\/\/rollbar.com\/([a-zA-Z0-9_\-\.]+\/[a-zA-Z0-9_\-\.]+)($|\/?.*)`)

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
			continue
		}
		//TODO: the occurrence data should be cached as it is less or more immutable
		occurrence, err := rollbar.GetOccurrenceData(item.ActivatingOccurrenceID, token)
		if err != nil {
			log.Printf("couldn't fetch occurrence data for %s: %s", link.URL, err.Error())
			//don't continue as we have the item info, even if without stack trace
		}
		linkData[link.URL] = getUnfurlData(item, occurrence)
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

func getUnfurlData(item *rollbar.Item, occurrence *rollbar.Occurrence) slackAttachment {
	now := time.Now()
	attachment := slackAttachment{
		Title:    item.Title,
		Fallback: item.Title,
		TS:       now.Unix(),
		Fields:   make([]slackAttachmentField, 5),
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

	if occurrence != nil && len(occurrence.Data.Body.TraceChain[0].Frames) > 0 {
		stacktrace := "```"
		totalFrames := len(occurrence.Data.Body.TraceChain[0].Frames)
		for i := 0; i < maxStacktraceFrames; i++ {
			index := totalFrames - i - 1
			if index < 0 {
				break
			}
			frame := occurrence.Data.Body.TraceChain[0].Frames[index]
			stacktrace += fmt.Sprintf("at %s.%s (%s:%d)\n", frame.ClassName, frame.Method, frame.Filename, frame.Lineno)
		}
		if totalFrames > maxStacktraceFrames {
			stacktrace += fmt.Sprintf("(... %d more frames ...)\n", totalFrames-maxStacktraceFrames)
		}
		stacktrace += "```"
		attachment.Fields[4] = slackAttachmentField{
			Title: "Stack trace",
			Value: stacktrace,
			Short: false,
		}
		attachment.MrkdwnIn = "fields"
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
