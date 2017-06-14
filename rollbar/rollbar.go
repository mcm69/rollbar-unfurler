package rollbar

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
)

type itemResponse struct {
	Err     int
	Result  Item
	Message string
}

// Item blah
type Item struct {
	ID                       int
	ProjectID                int `json:"project_id"`
	Environment              string
	Title                    string
	FirstOccurrenceTimestamp int `json:"first_occurrence_timestamp"`
	LastOccurrenceTimestamp  int `json:"last_occurrence_timestamp"`
	Status                   string
	TotalOccurrences         int `json:"total_occurrences"`
}

var re = regexp.MustCompile(`(\w+\/\w+)\/items\/(\d+)/?`)

// IsValidToken checks if token is a valid Rollbar read-level project token.
// Unfortunately, there is no way to see if a token belongs to project with a certain URL --
// we can only obtain project ID from the API if the token is valid, which is hardly helpful.
// So, the least we can do is check that token is a valid token and hope the user has copied
// it from the correct page.
func IsValidToken(token string) bool {
	if token == "" {
		return false
	}
	apiURL := fmt.Sprintf("https://api.rollbar.com/api/1/item/1?access_token=%s", token)
	resp, err := http.Get(apiURL)
	if err != nil {
		log.Printf("IsValidToken error: %s", err.Error())
		return false
	}
	defer resp.Body.Close()
	var result itemResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("IsValidToken error: %s", err.Error())
		return false
	}

	return result.Err == 0 || result.Message != "invalid access token"
}

// GetItemData sfes
func GetItemData(counter, token string) (*Item, error) {
	apiURL := fmt.Sprintf("https://api.rollbar.com/api/1/item_by_counter/%s?access_token=%s", counter, token)
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result itemResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Err != 0 {
		return nil, fmt.Errorf("API error: %s", result.Message)
	}
	return &result.Result, nil
}
