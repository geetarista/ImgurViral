package imgurviral

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"gopkg.in/tweetlib.v2"

	"appengine"
	"appengine/memcache"
	"appengine/taskqueue"
	"appengine/urlfetch"
)

var (
	twitterClient *tweetlib.Client
	imgurURL      = "https://api.imgur.com/3/gallery/hot/time/0.json"
)

var config struct {
	ImgurClientID            string `json:"imgurClientID"`
	ImgurClientSecret        string `json:"imgurClientSecret"`
	TwitterAPIKey            string `json:"twitterAPIKey"`
	TwitterAPISecret         string `json:"twitterAPISecret`
	TwitterAccessToken       string `json:"twitterAccessToken`
	TwitterAccessTokenSecret string `json:"twitterAccessTokenSecret"`
}

type Image struct {
	ID   string `json:"id"`
	Link string `json:"link"`
}

// https://api.imgur.com/models/gallery_image
// https://api.imgur.com/models/gallery_album
type Result struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Cover  string   `json:"cover,omitempty"`
	Images []*Image `json:"images"`
	Link   string   `json:"link,omitempty"`
}

type Results struct {
	Data []*Result `json:"data"`
}

func init() {
	file, err := os.Open("./conf.json")
	if err != nil {
		log.Println("Could not open conf.json")
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		log.Println("Error reading conf.json:", err)
	}

	http.HandleFunc("/tasks/poll", pollImgur)
	http.HandleFunc("/tasks/process", processTasks)
}

// Get latest images from Imgur
// https://api.imgur.com/endpoints/gallery
func pollImgur(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	req, err := http.NewRequest("GET", imgurURL, nil)
	if err != nil {
		log.Println("Error:", err)
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Client-ID %s", config.ImgurClientID))
	client := &http.Client{
		Transport: &urlfetch.Transport{Context: ctx},
	}
	res, err := client.Do(req)
	if err != nil {
		log.Println("Error:", err)
	}
	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)
	results := Results{}
	err = decoder.Decode(&results)
	if err != nil {
		log.Println("Error:", err)
	}

	for _, result := range results.Data {
		if _, err := memcache.Get(ctx, result.ID); err == memcache.ErrCacheMiss {
			// not in cache--do nothing
		} else if err != nil {
			ctx.Errorf("error getting item: %v", err)
		} else {
			break
		}

		title := result.Title
		titleLength := 90
		if len(title) > titleLength {
			title = title[:titleLength-1] + "…"
		}
		var status string
		if result.Cover != "" {
			status = title + " http://i.imgur.com/" + result.Cover + ".jpg" + " (" + result.Link + ")"
		} else {
			status = title + " " + result.Link + " (https://imgur.com/gallery/" + result.ID + ")"
		}

		t := &taskqueue.Task{
			Name:    result.ID,
			Payload: []byte(status),
			Method:  "PULL",
		}
		if _, err := taskqueue.Add(ctx, t, "pull-queue"); err != nil {
			log.Printf("Unable to add task for ID " + result.ID)
			break
		}
	}
}

func processTasks(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	tasks, err := taskqueue.Lease(ctx, 100, "pull-queue", 3600)
	if err != nil {
		log.Println("Unable to lease tasks from pull-queue")
		return
	}

	for _, task := range tasks {
		err := postTweet(ctx, task.Name, string(task.Payload))
		if err != nil {
			ctx.Errorf("Unable to post tweet %s", err)
		}

		item := &memcache.Item{
			Key:        task.Name,
			Expiration: 72 * time.Hour,
		}
		if err := memcache.Add(ctx, item); err != memcache.ErrNotStored {
			ctx.Errorf("error adding item: %v", err)
		}

		err = taskqueue.Delete(ctx, task, "pull-queue")
		if err != nil {
			ctx.Errorf("Unable to delete task")
			log.Println(err)
		}
	}
}

// Tweet a result
func postTweet(ctx appengine.Context, id, status string) (err error) {
	tweetlibConfig := &tweetlib.Config{
		ConsumerKey:    config.TwitterAPIKey,
		ConsumerSecret: config.TwitterAPISecret,
	}

	token := &tweetlib.Token{
		OAuthToken:  config.TwitterAccessToken,
		OAuthSecret: config.TwitterAccessTokenSecret,
	}

	tr := &tweetlib.Transport{
		Config:    tweetlibConfig,
		Token:     token,
		Transport: &urlfetch.Transport{Context: ctx},
	}

	twitterClient, err = tweetlib.New(tr.Client())
	if err != nil {
		log.Println("Error creating tweetlib client:", err)
		return
	}
	_, err = twitterClient.Tweets.Update(status, nil)
	if err != nil {
		log.Println("Error tweeting", err)
		return
	}

	return nil
}
