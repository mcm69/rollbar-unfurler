package rollbar

import (
	"encoding/json"
	"fmt"
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
