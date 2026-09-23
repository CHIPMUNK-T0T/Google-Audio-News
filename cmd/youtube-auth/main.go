// Command youtube-auth obtains a YouTube upload refresh token for the
// uploader. Run it once on your own computer:
//
//	YOUTUBE_CLIENT_ID=... YOUTUBE_CLIENT_SECRET=... go run ./cmd/youtube-auth
//
// The OAuth client must be of type "Desktop app", and the OAuth consent
// screen must be published ("In production"); refresh tokens issued while
// it is in "Testing" expire after 7 days.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	yt "google.golang.org/api/youtube/v3"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	clientID, clientSecret := os.Getenv("YOUTUBE_CLIENT_ID"), os.Getenv("YOUTUBE_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return errors.New("set YOUTUBE_CLIENT_ID and YOUTUBE_CLIENT_SECRET")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{yt.YoutubeUploadScope},
		RedirectURL:  fmt.Sprintf("http://%s/callback", listener.Addr()),
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return err
	}
	state := hex.EncodeToString(stateBytes)
	verifier := oauth2.GenerateVerifier()

	codes := make(chan string, 1)
	errs := make(chan error, 1)
	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			switch {
			case q.Get("state") != state:
				http.Error(w, "state mismatch", http.StatusBadRequest)
				errs <- errors.New("state mismatch")
			case q.Get("error") != "":
				http.Error(w, q.Get("error"), http.StatusBadRequest)
				errs <- fmt.Errorf("authorization failed: %s", q.Get("error"))
			default:
				fmt.Fprintln(w, "Authorized. You can close this tab.")
				codes <- q.Get("code")
			}
		}),
	}
	go server.Serve(listener)
	defer server.Close()

	fmt.Println("Open this URL in a browser signed in to the YouTube channel owner's account:")
	fmt.Println()
	fmt.Println(conf.AuthCodeURL(state,
		oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier)))
	fmt.Println()

	var code string
	select {
	case code = <-codes:
	case err := <-errs:
		return err
	case <-time.After(10 * time.Minute):
		return errors.New("timed out waiting for authorization")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}
	if token.RefreshToken == "" {
		return errors.New("no refresh token returned; revoke the app's access at https://myaccount.google.com/permissions and retry")
	}

	fmt.Println("Refresh token (store it in Secret Manager as youtube-refresh-token):")
	fmt.Println(token.RefreshToken)
	return nil
}
