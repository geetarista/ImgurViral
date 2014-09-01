package imgurviral

import (
	"bytes"
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
	ID    string `json:"id"`
	Title string `json:"title"`
	Cover string `json:"cover,omitempty"`
	Link  string `json:"link,omitempty"`
}

type ResultList []*Result

type Results struct {
	Data    ResultList `json:"data"`
	Success bool       `json:"success"`
	Status  int32      `json:"status"`
}

type appHandler func(http.ResponseWriter, *http.Request) error

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := fn(w, r); err != nil {
		http.Error(w, err.Error(), 500)
	}
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

	http.Handle("/tasks/poll", appHandler(pollImgur))
	http.Handle("/tasks/process", appHandler(processTasks))
}

// Get latest images from Imgur
// https://api.imgur.com/endpoints/gallery
func pollImgur(w http.ResponseWriter, r *http.Request) error {
	ctx := appengine.NewContext(r)
	req, err := http.NewRequest("GET", imgurURL, nil)
	if err != nil {
		ctx.Errorf("Unable to create Imgur request:", err)
		return err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Client-ID %s", config.ImgurClientID))
	client := &http.Client{
	// Transport: &urlfetch.Transport{Context: ctx, Deadline: 10 * time.Second},
	}
	res, err := client.Do(req)
	if err != nil {
		ctx.Errorf("Error fetching from Imgur: %s", err)
		return err
	}
	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)
	results := Results{}
	err = decoder.Decode(&results)
	if err != nil {
		ctx.Errorf("Error decoding Imgur results:", err)
		return err
	}

	if results.Success != true || results.Status != 200 {
		ctx.Errorf("unsuccessful request to imgur")
		return err
	}

	// Reverse the results since oldest is last
	for i := len(results.Data) - 1; i > -1; i-- {
		result := results.Data[i]
		// ctx.Infof("Processing result with ID %s", result.ID)
		if _, err := memcache.Get(ctx, result.ID); err == memcache.ErrCacheMiss {
			// not in cache--do nothing
		} else if err != nil {
			ctx.Errorf("error getting item with key %s: %v", result.ID, err)
			continue
		} else {
			// already in cache--skip
			continue
		}

		var data bytes.Buffer
		enc := json.NewEncoder(&data)
		err = enc.Encode(result)
		if err != nil {
			ctx.Errorf("Unable to encode result with ID %s", result.ID)
			continue
		}

		t := &taskqueue.Task{
			Name:    result.ID,
			Payload: data.Bytes(),
			Method:  "PULL",
		}
		if _, err := taskqueue.Add(ctx, t, "pull-queue"); err != nil && err != taskqueue.ErrTaskAlreadyAdded {
			ctx.Errorf("Unable to add task for ID %s: %s", result.ID, err)
			continue
		}
	}
	return nil
}

func processTasks(w http.ResponseWriter, r *http.Request) error {
	ctx := appengine.NewContext(r)
	tasks, err := taskqueue.Lease(ctx, 20, "pull-queue", 30)
	if err != nil {
		ctx.Errorf("Unable to lease tasks from pull-queue: %s", err)
		return err
	}
	// ctx.Infof("Length of tasks %d", len(tasks))

	for _, task := range tasks {
		// ctx.Infof("Processing task %s", task.Name)
		var result *Result
		err = json.Unmarshal(task.Payload, &result)
		if err != nil {
			ctx.Errorf("Unable to decode result with ID %s: %s", task.Name, err)
			continue
		}

		// download image
		// var file string
		// if result.Cover != "" {
		// 	file = "http://i.imgur.com/" + result.Cover + ".jpg"
		// } else {
		// 	file = result.Link
		// }
		// req, err := http.NewRequest("GET", file, nil)
		// if err != nil {
		// 	ctx.Errorf("Unable to create request: %s", err)
		// }
		// client := &http.Client{
		// 	Transport: &urlfetch.Transport{Context: ctx, Deadline: 10 * time.Second},
		// }
		// response, err := client.Do(req)
		// if err != nil {
		// 	ctx.Errorf("Error while downloading %s: %s", file, err)
		// 	continue
		// }
		// defer response.Body.Close()
		// image, err := ioutil.ReadAll(response.Body)
		// if err != nil {
		// 	ctx.Errorf("Error reading response for file %s", file)
		// 	continue
		// }
		// media := &tweetlib.TweetMedia{result.ID + ".jpg", image}
		// // Images can't be more than 3 MB
		// if len(image) > 3000000 {
		// 	media = nil
		// }
		media := &tweetlib.TweetMedia{result.ID + ".jpg", []byte{}}
		media = nil

		title := result.Title
		titleLength := 91
		if len(title) > titleLength {
			title = title[:titleLength-1] + "…"
		}
		var link string
		if result.Cover != "" {
			link = fmt.Sprintf("http://i.imgur.com/%s.jpg", result.Cover)
		} else {
			link = result.Link
		}
		status := fmt.Sprintf("%s %s (https://imgur.com/gallery/%s)", title, link, result.ID)
		// if media == nil {
		// 	status += " " + result.Link
		// }
		_, err = postTweet(ctx, status, media)
		if err != nil {
			ctx.Errorf("Unable to post tweet %s", err)
			continue
		}

		item := &memcache.Item{
			Key:        task.Name,
			Expiration: 72 * time.Hour,
			Value:      []byte(""),
		}
		if err := memcache.Add(ctx, item); err != nil && err != memcache.ErrNotStored {
			ctx.Errorf("error adding item with ID %s: %s", task.Name, err)
			continue
		}

		err = taskqueue.Delete(ctx, task, "pull-queue")
		if err != nil {
			ctx.Errorf("Unable to delete task ID %s: %s", task.Name, err)
			continue
		}
	}
	return nil
}

// Tweet a result
func postTweet(ctx appengine.Context, status string, image *tweetlib.TweetMedia) (tweet *tweetlib.Tweet, err error) {
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
		ctx.Errorf("Error creating tweetlib client: %s", err)
		return
	}
	if image == nil {
		return twitterClient.Tweets.Update(status, nil)
	} else {
		return twitterClient.Tweets.UpdateWithMedia(status, image, nil)
	}
}
