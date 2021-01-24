package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/kelseyhightower/envconfig"
)

type Env struct {
	Apikey     string // NewsAPI api key
	WebhookURL string // Slack webhook url
}

type RequestParameter struct {
	From             string
	To               string
	S3BacketName     string
	S3ObjectKey      string
	Keyword          string //This setting is for local environment.
	NoticeLowerLimit int    //This setting is for local environment.
}

type PickupKey struct {
	Keyword string `json:"keyword"`

	// Don't notify if the number of news is below NoticeLowerLimit
	NoticeLowerLimit int `json:"noticeLowerLimit"`
}

func main() {
	lambda.Start(HandleRequest)
}

func HandleRequest(ctx context.Context, rp RequestParameter) (string, error) {
	// import enviroment
	var env Env
	if err := envconfig.Process("pickupnews", &env); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	p := initRequestParameter(&rp)
	keys := loadPickupKeys(&rp)

	// create request
	resuest, err := http.NewRequest("GET", "http://newsapi.org/v2/everything", nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, key := range *keys {
		values := url.Values{}
		values.Add("qInTitle", key.Keyword)
		values.Add("from", p.From)
		values.Add("to", p.To)
		values.Add("apiKey", env.Apikey)
		resuest.URL.RawQuery = values.Encode()

		// execute NewsAPI
		client := new(http.Client)
		resp, err := client.Do(resuest)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		} else if resp.StatusCode != 200 {
			fmt.Printf("Unable to get this url : http status is %d \n", resp.StatusCode)
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		naResp := new(NewsAPIRespons)
		if err := json.Unmarshal(body, &naResp); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if naResp.TotalResults <= key.NoticeLowerLimit {
			return fmt.Sprintf("TotalResult is lower NoticeLowerLimit. TotalResult:%d, NoticeLowerLimit:%d\n", naResp.TotalResults, key.NoticeLowerLimit), nil
		}

		messageHeader := "<!channel> Keyword: " + key.Keyword + " resultCount: " + strconv.Itoa(naResp.TotalResults) + " from: " + p.From + " to: " + p.To + "\n"
		var messageDetail bytes.Buffer
		for i, article := range naResp.Articles {
			messageDetail.WriteString("No.")
			messageDetail.WriteString(strconv.Itoa(i + 1))
			messageDetail.WriteString(", ")
			messageDetail.WriteString(article.Title)
			messageDetail.WriteString(", ")
			messageDetail.WriteString(article.URL)
			messageDetail.WriteString("\n")
		}

		notificationSlack(env, messageHeader+messageDetail.String())
	}

	return "Success notification.", nil
}

func initRequestParameter(rp *RequestParameter) *RequestParameter {
	t := time.Now().UTC()
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		loc = time.FixedZone("Asia/Tokyo", 9*60*60)
	}
	t = t.In(loc)

	if rp.From == "" {
		rp.From = t.AddDate(0, 0, -1).Format("2006-01-02") // Previous day
	}

	if rp.To == "" {
		rp.To = t.Format("2006-01-02") // The day
	}
	return rp
}

func notificationSlack(env Env, message string) {
	params := `{"text":"` + message + `"}`
	resuest, err := http.NewRequest("POST", env.WebhookURL, bytes.NewBuffer([]byte(params)))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	resuest.Header.Set("Content-Type", "application/json")

	// Execute slack webhook
	client := new(http.Client)
	resp, err := client.Do(resuest)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else if resp.StatusCode != 200 {
		fmt.Printf("Unable to post this url : http status is %d \n", resp.StatusCode)
	}
	defer resp.Body.Close()
}

func loadPickupKeys(rp *RequestParameter) *[]PickupKey {
	if rp.S3BacketName == "" || rp.S3ObjectKey == "" {
		pk := PickupKey{
			Keyword:          rp.Keyword,
			NoticeLowerLimit: rp.NoticeLowerLimit,
		}
		return &[]PickupKey{pk}
	}

	jsonBytes := readS3File(rp)
	var pickupKeys []PickupKey
	if err := json.Unmarshal(jsonBytes, &pickupKeys); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	return &pickupKeys
}

func readS3File(rp *RequestParameter) []byte {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		Profile:           "di",
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := s3.New(sess)

	obj, err := svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(rp.S3BacketName),
		Key:    aws.String(rp.S3ObjectKey),
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer obj.Body.Close()
	brb := new(bytes.Buffer)
	brb.ReadFrom(obj.Body)
	return brb.Bytes()
}

type NewsAPIRespons struct {
	Status       string `json:"status"`
	TotalResults int    `json:"totalResults"`
	Articles     []struct {
		Source struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"source"`
		Author      string    `json:"author"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		URL         string    `json:"url"`
		URLToImage  string    `json:"urlToImage"`
		PublishedAt time.Time `json:"publishedAt"`
		Content     string    `json:"content"`
	} `json:"articles"`
}
