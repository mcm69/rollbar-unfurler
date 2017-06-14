package main

import (
	"fmt"
	"log"
	"net/http"

	"os"

	"strconv"

	"io/ioutil"

	"./db"
)

type Config struct {
	// Host for the app to listen on. May be empty to listen on all interfaces
	ListenHost string
	// Port for the app to listen on. Default 8888
	ListenPort             int
	ClientID               string
	ClientSecret           string
	SlackVerificationToken string
}

var config Config

func loadConfig() {
	port, _ := strconv.Atoi(os.Getenv("UNFURLER_PORT"))

	config = Config{
		ListenHost:             os.Getenv("UNFURLER_HOST"),
		ListenPort:             port,
		ClientID:               os.Getenv("UNFURLER_CLIENT_ID"),
		ClientSecret:           os.Getenv("UNFURLER_CLIENT_SECRET"),
		SlackVerificationToken: os.Getenv("UNFURLER_VERIFICATION_TOKEN"),
	}

	// set defaults and validate
	if config.ListenPort == 0 {
		config.ListenPort = 8888
	}

	if config.ClientID == "" {
		log.Fatal("UNFURLER_CLIENT_ID is not set")
	}
	if config.ClientSecret == "" {
		log.Fatal("UNFURLER_CLIENT_SECRET is not set")
	}
	if config.SlackVerificationToken == "" {
		log.Fatal("UNFURLER_VERIFICATION_TOKEN is not set")
	}
}

func serveFile(w http.ResponseWriter, path string) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		log.Printf("Could not serve file %s: %s", path, err.Error())
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write(content)
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

func main() {
	loadConfig()
	db.Init()
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, "static/index.html")
	})
	http.HandleFunc("/slack", slackEventHandler)
	http.HandleFunc("/oauth", oauthCallbackHandler)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%d", config.ListenHost, config.ListenPort), nil))
}
