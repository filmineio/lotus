package marketauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	logging "github.com/ipfs/go-log/v2"
)

const tokenFile = "sealing_market_tokens.json"

var log = logging.Logger("sectors")

var (
	ValidationError  = errors.New("token validation error")
	pollTime         = 10 * time.Minute
	refreshThreshold = 30 * time.Minute
)

type TokensResponse struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
	TTL     int    `json:"ttl"` //time to live in seconds
}

type Tokens struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh"`
	Expires time.Time `json:"expires"` // when are the tokens going to expire
}

type AuthService struct {
	marketUri string
	f         *os.File // file handle for storing and loading tokens
	mtx       sync.Mutex
	tokens    *Tokens
	quit      chan struct{}
}

func New(marketUri, workingDir string) (*AuthService, error) {
	a := &AuthService{
		marketUri: marketUri,
		tokens:    &Tokens{},
		quit:      make(chan struct{}),
	}

	// Replace "~" with the user's home directory path
	if strings.HasPrefix(workingDir, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("unable to get user home directory: %w", err)
		}
		workingDir = strings.Replace(workingDir, "~", homeDir, 1)
	}

	// Ensure the directory exists
	if _, err := os.Stat(workingDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("working directory does not exist: %s", workingDir)
	}

	f, err := os.OpenFile(path.Join(workingDir, tokenFile), os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, fmt.Errorf("open tokens file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("token file stat: %w", err)
	}
	if info.Size() > 0 {
		// try to read the file
		b, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("read tokens file: %w", err)
		}

		_, err = f.Seek(0, 0)
		if err != nil {
			return nil, fmt.Errorf("seek to start: %w", err)
		}

		err = json.Unmarshal(b, a.tokens)
		if err != nil {
			return nil, fmt.Errorf("unmarshal tokens: %w", err)
		}
	}
	a.f = f
	return a, nil
}

// AccessToken decorates a given http.Request with the necessary bearer
// token that is needed to operate with the markets backend.
func (s *AuthService) AccessToken(req *http.Request) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", s.tokens.Access))
}

func (s *AuthService) PollRefresh(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollTime):
		}
		if time.Until(s.tokens.Expires) <= refreshThreshold {
			tokens, err := s.Refresh(s.tokens.Refresh)
			if err != nil {
				log.Warnf("got an error when refreshing tokens: %v", err)
				continue // try again on next poll
			}
			s.mtx.Lock()
			s.tokens = &tokens
			s.mtx.Unlock()
		}
		if err := s.Verify(s.tokens.Access); err != nil {
			log.Warnf("got an error when validating access token: %s: %v", s.tokens.Access, err) // for now this is non fatal
		}
	}
}

func (s *AuthService) Close() {
	close(s.quit)
}

// Register calls the market with the given OTP and registers the worker as
// an appliance. This is called from a short-lived CLI command which does not
// start the worker daemon thereafter. Therefore the tokens are written directly to a
// file, such that the worker could use them when in daemon mode.
func (s *AuthService) Register(otp string) (Tokens, error) {
	resp, err := http.Post(registerUri(s.marketUri, otp), "text/plain", nil)
	if err != nil {
		return Tokens{}, fmt.Errorf("posting otp to market: %w", err)
	}
	return s.processTokenResponse(resp)
}

func (s *AuthService) Verify(accessToken string) error {
	req, err := http.NewRequest(http.MethodGet, verifyUri(s.marketUri), nil)
	if err != nil {
		return fmt.Errorf("create verify request: %w", err)
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("get verify: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ValidationError
	}

	return nil
}

func (s *AuthService) Refresh(refreshToken string) (Tokens, error) {
	req, err := http.NewRequest(http.MethodPost, refreshUri(s.marketUri), nil)
	if err != nil {
		return Tokens{}, fmt.Errorf("create refresh request: %w", err)
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", refreshToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Tokens{}, fmt.Errorf("get verify: %w", err)
	}
	return s.processTokenResponse(resp)
}

func (s *AuthService) processTokenResponse(resp *http.Response) (Tokens, error) {
	if resp.StatusCode != http.StatusOK {
		return Tokens{}, fmt.Errorf("invalid backend token response code: %d", resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return Tokens{}, fmt.Errorf("read response body: %w", err)
	}

	tokens := TokensResponse{}
	err = json.Unmarshal(respBytes, &tokens)
	if err != nil {
		return Tokens{}, fmt.Errorf("unmarshal tokens: %w", err)
	}

	persistedTokens := Tokens{
		Access:  tokens.Access,
		Refresh: tokens.Refresh,
		Expires: time.Now().Add(time.Second * time.Duration(tokens.TTL)),
	}

	log.Debugf("got tokens from backend. expire at %s (%d seconds from now)", persistedTokens.Expires, tokens.TTL)

	b, err := json.Marshal(persistedTokens)
	if err != nil {
		return Tokens{}, fmt.Errorf("marshal tokens to persist: %w", err)
	}

	err = s.f.Truncate(0)
	if err != nil {
		return Tokens{}, fmt.Errorf("truncate tokens file: %w", err)
	}

	n, err := s.f.Write(b)
	if err != nil {
		return Tokens{}, fmt.Errorf("write tokens: %w", err)
	}
	if n < len(b) {
		return Tokens{}, fmt.Errorf("short write tokens")
	}

	return persistedTokens, nil

}

func registerUri(server, otp string) string {
	return fmt.Sprintf("%s/appliance/register/%s", server, otp)
}
func verifyUri(server string) string {
	return fmt.Sprintf("%s/appliance/verify", server)
}
func refreshUri(server string) string {
	return fmt.Sprintf("%s/appliance/refresh", server)
}
