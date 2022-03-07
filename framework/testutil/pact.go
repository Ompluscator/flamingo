package testutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pact-foundation/pact-go/dsl"
	"github.com/pact-foundation/pact-go/types"
)

// ErrNoPact error
var ErrNoPact = errors.New("no pact setup")

// WithPact runs a test with a pact
func WithPact(t *testing.T, from, to string, fs ...func(*testing.T, *dsl.Pact)) {
	if from == "" {
		from = "flamingo"
	}

	pact := pactSetup(from, to)
	// defer the pact teardown
	defer func() {
		if err := pactTeardown(pact); err != nil {
			t.Error(err)
		}
	}()

	for i, f := range fs {
		t.Run("Pact-"+strconv.Itoa(i), func(t *testing.T) { f(t, pact) })
	}
}

// pactSetup sets up pact environment for go tests
func pactSetup(consumer, provider string) *dsl.Pact {
	var pact = &dsl.Pact{
		Consumer: consumer,
		Provider: provider,
		LogLevel: "WARN",
	}

	pact.Setup(true)
	return pact
}

// pactTeardown tears down the pact instance
func pactTeardown(pact *dsl.Pact) error {
	if err := pact.WritePact(); err != nil {
		return err
	}

	defer pact.Teardown()
	if pactbroker := os.Getenv("PACT_BROKER_HOST"); pactbroker != "" {
		// Write pact to file `<pact-go>/pacts/my_consumer-my_provider.json`
		if err := pact.WritePact(); err != nil {
			return err
		}

		p := dsl.Publisher{}
		file := filepath.Join(pact.PactDir, fmt.Sprintf("%s-%s.json", strings.ToLower(pact.Consumer), strings.ToLower(pact.Provider)))

		err := p.Publish(types.PublishRequest{
			PactURLs:        []string{file},
			PactBroker:      strings.TrimSuffix(pactbroker, "/"),
			ConsumerVersion: os.Getenv("PACT_VERSION"),
			Tags:            strings.Split(os.Getenv("PACT_TAGS"), ","),
			BrokerUsername:  os.Getenv("PACT_BROKER_USERNAME"),
			BrokerPassword:  os.Getenv("PACT_BROKER_PASSWORD"),
			BrokerToken:     os.Getenv("PACT_BROKER_TOKEN"),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// PactEncodeLike encodes a byte slice from json.Marshal or jsonpb into a pact type-like representation
func PactEncodeLike(model interface{}) dsl.Matcher {
	payload, _ := json.Marshal(model)
	var data interface{}
	json.Unmarshal(payload, &data)

	return pactEncode(data)
}

func pactEncode(data interface{}) dsl.Matcher {
	switch data := data.(type) {
	case string:
		return dsl.Like(data)

	case int, float32, float64, bool, uint:
		return dsl.Like(data)

	case map[string]interface{}:
		result := dsl.StructMatcher{}
		for k, v := range data {
			result[k] = pactEncode(v)
		}
		return result

	case []interface{}:
		if len(data) < 1 {
			return dsl.EachLike(`null`, 0)
		}

		return dsl.EachLike(pactEncode(data[0]), len(data))

	case nil:
		return dsl.Like("null")
	}

	panic(fmt.Sprintf("can not encode %T", data))
}
