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
	ID                       int         `json:"id"`
	ProjectID                int         `json:"project_id"`
	Counter                  int         `json:"counter"`
	Environment              string      `json:"environment"`
	Platform                 string      `json:"platform"`
	Framework                string      `json:"framework"`
	Hash                     string      `json:"hash"`
	Title                    string      `json:"title"`
	FirstOccurrenceID        int64       `json:"first_occurrence_id"`
	FirstOccurrenceTimestamp int         `json:"first_occurrence_timestamp"`
	ActivatingOccurrenceID   int64       `json:"activating_occurrence_id"`
	LastActivatedTimestamp   int         `json:"last_activated_timestamp"`
	LastResolvedTimestamp    interface{} `json:"last_resolved_timestamp"`
	LastMutedTimestamp       interface{} `json:"last_muted_timestamp"`
	LastOccurrenceID         int64       `json:"last_occurrence_id"`
	LastOccurrenceTimestamp  int         `json:"last_occurrence_timestamp"`
	TotalOccurrences         int         `json:"total_occurrences"`
	LastModifiedBy           int         `json:"last_modified_by"`
	Status                   string      `json:"status"`
	Level                    string      `json:"level"`
	IntegrationsData         interface{} `json:"integrations_data"`
	AssignedUserID           interface{} `json:"assigned_user_id"`
	GroupItemID              interface{} `json:"group_item_id"`
	GroupStatus              int         `json:"group_status"`
}

// Occurrence is the JSON representation of a Rollbar occurrence
type Occurrence struct {
	ID        int64 `json:"id"`
	ProjectID int   `json:"project_id"`
	Timestamp int   `json:"timestamp"`
	Version   int   `json:"version"`
	Data      struct {
		Server struct {
			IP          string `json:"ip"`
			CodeVersion string `json:"code_version"`
			Host        string `json:"host"`
		} `json:"server"`
		Level    string `json:"level"`
		Language string `json:"language"`
		Body     struct {
			TraceChain []struct {
				Exception struct {
					Message string `json:"message"`
					Class   string `json:"class"`
				} `json:"exception"`
				Frames []struct {
					Filename  string `json:"filename"`
					Lineno    int    `json:"lineno"`
					Method    string `json:"method"`
					ClassName string `json:"class_name"`
				} `json:"frames"`
			} `json:"trace_chain"`
		} `json:"body"`
		Platform    string `json:"platform"`
		Environment string `json:"environment"`
		Framework   string `json:"framework"`
		Timestamp   int    `json:"timestamp"`
		UUID        string `json:"uuid"`
	} `json:"data"`
}

type occurrenceResponse struct {
	Err     int
	Result  Occurrence
	Message string
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

// GetOccurrenceData aegaa
func GetOccurrenceData(id, token string) (*Occurrence, error) {
	apiURL := fmt.Sprintf("https://api.rollbar.com/api/1/instance/%s?access_token=%s", id, token)
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result occurrenceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Err != 0 {
		return nil, fmt.Errorf("API error: %s", result.Message)
	}

	return &result.Result, nil
}
