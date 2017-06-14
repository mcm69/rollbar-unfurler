package db

import "github.com/boltdb/bolt"
import "log"

import "fmt"

var usersBucket = []byte("users")
var projectsBucket = []byte("projects")

var db *bolt.DB

func Init() {
	var err error
	db, err = bolt.Open("unfurler.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func addTeam(teamID string) {
	err := db.Update(func(tx *bolt.Tx) error {
		//create team bucket
		teamBucket, err := tx.CreateBucketIfNotExists([]byte(teamID))
		if err != nil {
			return err
		}

		//create users sub-bucket
		_, err = teamBucket.CreateBucketIfNotExists(usersBucket)
		if err != nil {
			return err
		}

		//create projects sub-bucket
		_, err = teamBucket.CreateBucketIfNotExists(projectsBucket)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func GetAuthToken(teamName string) string {
	result := ""

	err := db.View(func(tx *bolt.Tx) error {
		teamBucket := tx.Bucket([]byte(teamName))
		if teamBucket == nil {
			return fmt.Errorf("Team %s is not registered", teamName)
		}

		usersBucket := teamBucket.Bucket(usersBucket)
		//just get the first user in the bucket
		user, token := usersBucket.Cursor().First()

		if user == nil {
			return fmt.Errorf("No users registered for team %s", teamName)
		}

		result = string(token)

		return nil
	})

	if err != nil {
		log.Printf("GetAuthToken: %s", err.Error())
	}

	return result
}

func GetProjectToken(teamName, project string) string {
	result := ""

	err := db.View(func(tx *bolt.Tx) error {
		teamBucket := tx.Bucket([]byte(teamName))
		if teamBucket == nil {
			return fmt.Errorf("Team %s is not registered", teamName)
		}

		projectsBucket := teamBucket.Bucket(projectsBucket)
		result = string(projectsBucket.Get([]byte(project)))

		return nil
	})

	if err != nil {
		log.Printf("GetProjectToken: %s", err.Error())
	}

	return result
}

func DeleteUserToken(teamName, user string) {
	err := db.Update(func(tx *bolt.Tx) error {
		teamBucket := tx.Bucket([]byte(teamName))
		if teamBucket == nil {
			return fmt.Errorf("Team %s is not registered", teamName)
		}
		usersBucket := teamBucket.Bucket(projectsBucket)
		return usersBucket.Delete([]byte(user))
	})

	if err != nil {
		log.Printf("DeleteUserToken: %s", err.Error())
	}
}

func DeleteTeam(teamName string) {
	err := db.Update(func(tx *bolt.Tx) error {
		return tx.DeleteBucket([]byte(teamName))
	})

	if err != nil {
		log.Printf("DeleteTeam: %s", err.Error())
	}
}