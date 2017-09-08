package provider

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/pact-foundation/pact-go/dsl"
	examples "github.com/pact-foundation/pact-go/examples/types"
	"github.com/pact-foundation/pact-go/types"
	"github.com/pact-foundation/pact-go/utils"
	"github.com/spf13/viper"
	"strings"
	"time"
)

var myClient = &http.Client{Timeout: 10 * time.Second}

// The actual Provider test itself
func TestPact_Provider(t *testing.T) {
	go startInstrumentedProvider()

	viper.AutomaticEnv()
	viper.SetDefault("PACT_BROKER_URL", "http://pact-broker.keyshift.co:80")
	viper.SetDefault("PACT_TARGET_ENV", "master")
	viper.SetDefault("CONSUMER", "<ALL>")
	var brokerHost = viper.GetString("PACT_BROKER_URL")
	var targetEnv = viper.GetString("PACT_TARGET_ENV")
	var consumer = viper.GetString("CONSUMER")

	var err error
	pact := createPact()

	pactURLs, err := getPactURLs(brokerHost, targetEnv, consumer)

	t.Log(pactURLs)

	if err != nil {
		t.Fatal("Error:", err)
	}

	// Verify the Provider - Tag-based Published Pacts for any known consumers
	err = pact.VerifyProvider(types.VerifyRequest{
		ProviderBaseURL:            fmt.Sprintf("http://127.0.0.1:%d", port),
		ProviderStatesSetupURL:     fmt.Sprintf("http://127.0.0.1:%d/setup", port),
		PactURLs:                   pactURLs, //[]string{fmt.Sprintf("%s/pacts/provider/bobby/consumer/billy/version/1.0.0", brokerHost)},
		BrokerURL:                  brokerHost,
		BrokerUsername:             os.Getenv("PACT_BROKER_USERNAME"),
		BrokerPassword:             os.Getenv("PACT_BROKER_PASSWORD"),
		PublishVerificationResults: true,
		ProviderVersion:            "1.0.0",
	})

	if err != nil {
		t.Fatal("Error:", err)
	}
}

// Starts the provider API with hooks for provider states.
// This essentially mirrors the main.go file, with extra routes added.
func startInstrumentedProvider() {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/login", UserLogin)
	mux.HandleFunc("/setup", providerStateSetupFunc)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	log.Printf("API starting: port %d (%s)", port, ln.Addr())
	log.Printf("API terminating: %v", http.Serve(ln, mux))

}

// Set current provider state route.
var providerStateSetupFunc = func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	var state types.ProviderState

	body, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()

	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	err = json.Unmarshal(body, &state)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// Setup database for different states
	if state.State == "User billy exists" {
		userRepository = billyExists
	} else if state.State == "User billy is unauthorized" {
		userRepository = billyUnauthorized
	} else {
		userRepository = billyDoesNotExist
	}
}

// Configuration / Test Data
var dir, _ = os.Getwd()
var pactDir = fmt.Sprintf("%s/../../pacts", dir)
var logDir = fmt.Sprintf("%s/log", dir)
var port, _ = utils.GetFreePort()

// Provider States data sets
var billyExists = &examples.UserRepository{
	Users: map[string]*examples.User{
		"billy": &examples.User{
			Name:     "billy",
			Username: "billy",
			Password: "issilly",
			Type:     "admin",
		},
	},
}

var billyDoesNotExist = &examples.UserRepository{}

var billyUnauthorized = &examples.UserRepository{
	Users: map[string]*examples.User{
		"billy": &examples.User{
			Name:     "billy",
			Username: "billy",
			Password: "issilly1",
			Type:     "blocked",
		},
	},
}

// Setup the Pact client.
func createPact() dsl.Pact {
	// Create Pact connecting to local Daemon
	return dsl.Pact{
		Port:     6666,
		Consumer: "billy",
		Provider: "bobby",
		LogDir:   logDir,
		PactDir:  pactDir,
	}
}

func getPactURLs(brokerHost string, targetEnv string, consumer string) ([]string, error) {

	pactLinks := new(PactLinks)

	err := getJson(fmt.Sprintf("%s/pacts/provider/bobby/latest/%s", brokerHost, targetEnv), pactLinks)

	if err != nil {
		return nil, err
	}

	var pactURLs = make([]string, 0)
	for _, link := range pactLinks.Pacts {
		if consumer == "<all>" || strings.Contains(link.HRef, fmt.Sprintf("/consumer/%s", consumer)) {
			pactURLs = append(pactURLs, link.HRef)
		}
	}

	return pactURLs, nil
}

type Link struct {
	HRef string `json:"href"`
}

type PactListResponse struct {
	Links PactLinks `json:"_links"`
}

type PactLinks struct {
	Self     Link   `json:"self"`
	Provider Link   `json:"provider"`
	Pacts    []Link `json:"pacts"`
}

func getJson(url string, target interface{}) error {
	r, err := myClient.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(target)
}
