package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"gopkg.in/yaml.v3"
)

type Configuration struct {
	Contracts map[string]string `yaml:"contracts"` // contract-id: alias
}

// getConfiguration loads YAML data from a file and returns a Configuration object
func getConfiguration(filename string) (*Configuration, error) {
	yamlData, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return &Configuration{}, nil
		}
		return nil, fmt.Errorf("error reading file: %w", err)
	}
	var config Configuration
	err = yaml.Unmarshal(yamlData, &config)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling YAML: %w", err)
	}
	return &config, nil
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		config.RedirectURL = "http://localhost:8080/oauth2/callback"
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)

	// Parse the RedirectURL to get the callback path and server address.
	redirectURL, err := url.Parse(config.RedirectURL)
	if err != nil {
		log.Fatalf("Unable to parse RedirectURL: %v", err)
	}
	serverAddr := redirectURL.Host
	callbackPath := redirectURL.Path

	// Create a channel to receive the authorization code.
	codeChan := make(chan string)

	// Start a temporary HTTP server to handle the OAuth callback.
	server := &http.Server{Addr: serverAddr}
	http.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		codeChan <- code
		fmt.Fprintf(w, "Authorization successful! You can close this window now.")
		go func() {
			server.Shutdown(context.Background())
		}()
	})
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server ListenAndServe: %v", err)
		}
	}()

	// Open the default browser.
	log.Printf("Opening browser for authorization at %v...", authURL)
	err = openBrowser(authURL)
	if err != nil {
		log.Printf("Unable to open browser automatically: %v", err)
		fmt.Printf("Please open the following URL in your browser:\n%v\n", authURL)
	}

	// Wait for the authorization code.
	authCode := <-codeChan
	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

func openBrowser(url string) error {
	var cmd = exec.Command("xdg-open", url)
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start command: %v", err)
	}
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("Browser command encountered an error: %v", err)
		}
	}()
	return nil
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// encodeQuery encodes a map into a Gmail search query string.
// see https://developers.google.com/gmail/api/reference/rest/v1/users.messages/list
func encodeQuery(params map[string]string) string {
	var queries []string
	for key, value := range params {
		if strings.ContainsAny(value, " :") {
			queries = append(queries, fmt.Sprintf("%s:\"%s\"", key, value))
		} else {
			queries = append(queries, fmt.Sprintf("%s:%s", key, value))
		}
	}
	return strings.Join(queries, " ")
}

// formatDate formats the Unix timestamp in milliseconds to YYYY-MM-DD.
func formatDate(internalDateMs int64) string {
	t := time.Unix(internalDateMs/1000, (internalDateMs%1000)*int64(time.Millisecond))
	return t.Format("2006-01-02")
}

func saveAttachment(filename string, part *gmail.MessagePartBody) error {
	data, err := base64.URLEncoding.DecodeString(part.Data)
	if err != nil {
		return fmt.Errorf("unable to decode attachment data: %v", err)
	}
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("unable to write attachment file: %v", err)
	}
	return nil
}

func saveRawMessage(filename, rawContent string) error {
	decodedContent, err := base64.URLEncoding.DecodeString(rawContent)
	if err != nil {
		return fmt.Errorf("unable to decode message content: %v", err)
	}
	err = os.WriteFile(filename, decodedContent, 0644)
	if err != nil {
		return fmt.Errorf("unable to write raw message file: %v", err)
	}
	return nil
}

func main() {
	configuration, err := getConfiguration("config.yaml")
	if err != nil {
		log.Fatalf("Unable to read the configuration: %v", err)
	}

	ctx := context.Background()
	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	config, err := google.ConfigFromJSON(b, gmail.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	// e.g. A sua fatura EDP (contrato 100200300200)
	contractSubjectRegex := regexp.MustCompile(`\(contrato (\d+)\)`)

	// e.g. 187008571923.pdf
	invoiceFilenameRegex := regexp.MustCompile(`^\d+\.pdf$`)

	user := "me"

	// see search google for "Gmail search box"
	// see Refine searches in Gmail at https://support.google.com/mail/answer/7190?hl=en
	searchParams := map[string]string{
		"from": "faturaedp@edp.pt",
		"has":  "attachment",
	}
	q := encodeQuery(searchParams)

	messageIndex := 0
	pageToken := ""
	for {
		// see Refine searches in Gmail at https://support.google.com/mail/answer/7190?hl=en
		// see https://developers.google.com/gmail/api/reference/rest/v1/users.messages/list
		req := srv.Users.Messages.List(user).Q(q)
		if pageToken != "" {
			req.PageToken(pageToken)
		}

		r, err := req.Do()
		if err != nil {
			log.Fatalf("Unable to retrieve messages: %v", err)
		}

		if len(r.Messages) == 0 {
			break
		}

		for _, m := range r.Messages {
			// see https://developers.google.com/gmail/api/reference/rest/v1/users.messages/get
			msg, err := srv.Users.Messages.Get(user, m.Id).Format("full").Do()
			if err != nil {
				log.Printf("Unable to retrieve message %v: %v", m.Id, err)
				continue
			}

			date := formatDate(msg.InternalDate)
			var from string
			var subject string
			for _, header := range msg.Payload.Headers {
				switch header.Name {
				case "Subject":
					subject = header.Value
				case "From":
					from = header.Value
				}
			}

			fmt.Printf("#%08d %s %s %s: %s\n", messageIndex, m.Id, date, from, subject)

			filenamePrefix := date + "-" + m.Id

			matches := contractSubjectRegex.FindStringSubmatch(subject)
			if len(matches) > 1 {
				contract := matches[1]
				filenamePrefix = fmt.Sprintf("%s-edp-%s", date, contract)
				if alias, ok := configuration.Contracts[contract]; ok {
					filenamePrefix = fmt.Sprintf("%s-%s", filenamePrefix, alias)
				}
			}

			for _, part := range msg.Payload.Parts {
				if part.MimeType == "application/pdf" {
					if invoiceFilenameRegex.MatchString(part.Filename) {
						attachMsg, err := srv.Users.Messages.Attachments.Get(user, m.Id, part.Body.AttachmentId).Do()
						if err != nil {
							log.Printf("Unable to retrieve message %v attachment %v: %v", m.Id, part.Body.AttachmentId, err)
							continue
						}
						err = saveAttachment(filenamePrefix+"-"+part.Filename, attachMsg)
						if err != nil {
							log.Printf("Unable to save message %v attachment %v: %v", m.Id, part.Body.AttachmentId, err)
							continue
						}
					}
				}
			}

			// see https://developers.google.com/gmail/api/reference/rest/v1/users.messages/get
			rawMsg, err := srv.Users.Messages.Get(user, m.Id).Format("raw").Do()
			if err != nil {
				log.Printf("Unable to retrieve raw message %v: %v", m.Id, err)
				continue
			}
			err = saveRawMessage(filenamePrefix+".eml", rawMsg.Raw)
			if err != nil {
				log.Printf("Error saving message %v: %v", m.Id, err)
			}

			messageIndex++
		}

		if r.NextPageToken == "" {
			break
		}
		pageToken = r.NextPageToken
	}
}
