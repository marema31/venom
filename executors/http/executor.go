package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/fsamin/go-dump"
	"github.com/mitchellh/mapstructure"

	"github.com/runabove/venom"
)

// Name of executor
const Name = "http"

// New returns a new Executor
func New() venom.Executor {
	return &Executor{}
}

// Headers represents header HTTP for Request
type Headers map[string]string

// Executor struct
type Executor struct {
	Method        string      `json:"method" yaml:"method"`
	URL           string      `json:"url" yaml:"url"`
	Path          string      `json:"path" yaml:"path"`
	Body          string      `json:"body" yaml:"body"`
	MultipartForm interface{} `json:"multipart_form" yaml:"multipart_form"`
	Headers       Headers     `json:"headers" yaml:"headers"`
}

// Result represents a step result
type Result struct {
	Executor    Executor    `json:"executor,omitempty" yaml:"executor,omitempty"`
	TimeSeconds float64     `json:"timeSeconds,omitempty" yaml:"timeSeconds,omitempty"`
	TimeHuman   string      `json:"timeHuman,omitempty" yaml:"timeHuman,omitempty"`
	StatusCode  int         `json:"statusCode,omitempty" yaml:"statusCode,omitempty"`
	Body        string      `json:"body,omitempty" yaml:"body,omitempty"`
	BodyJSON    interface{} `json:"bodyjson,omitempty" yaml:"bodyjson,omitempty"`
	Headers     Headers     `json:"headers,omitempty" yaml:"headers,omitempty"`
	Err         error       `json:"error,omitempty" yaml:"error,omitempty"`
}

// GetDefaultAssertions return default assertions for this executor
// Optional
func (Executor) GetDefaultAssertions() venom.StepAssertions {
	return venom.StepAssertions{Assertions: []string{"result.statusCode ShouldEqual 200"}}
}

// Run execute TestStep
func (Executor) Run(l *log.Entry, aliases venom.Aliases, step venom.TestStep) (venom.ExecutorResult, error) {

	// transform step to Executor Instance
	var t Executor
	if err := mapstructure.Decode(step, &t); err != nil {
		return nil, err
	}

	// dirty: mapstructure doesn't like decoding map[interface{}]interface{}, let's force manually
	t.MultipartForm = step["multipart_form"]

	r := Result{Executor: t}

	req, err := t.getRequest()
	if err != nil {
		return nil, err
	}

	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	elapsed := time.Since(start)
	r.TimeSeconds = elapsed.Seconds()
	r.TimeHuman = fmt.Sprintf("%s", elapsed)

	var bb []byte
	if resp.Body != nil {
		defer resp.Body.Close()
		var errr error
		bb, errr = ioutil.ReadAll(resp.Body)
		if errr != nil {
			return nil, errr
		}
		r.Body = string(bb)

		bodyJSONArray := []interface{}{}
		if err := json.Unmarshal(bb, &bodyJSONArray); err != nil {
			bodyJSONMap := map[string]interface{}{}
			if err2 := json.Unmarshal(bb, &bodyJSONMap); err2 == nil {
				r.BodyJSON = bodyJSONMap
			}
		} else {
			r.BodyJSON = bodyJSONArray
		}
	}

	r.Headers = make(map[string]string)

	for k, v := range resp.Header {
		r.Headers[k] = v[0]
	}

	r.StatusCode = resp.StatusCode
	return dump.ToMap(r, dump.WithDefaultLowerCaseFormatter())
}

// getRequest returns the request correctly set for the current executor
func (e Executor) getRequest() (*http.Request, error) {
	path := fmt.Sprintf("%s%s", e.URL, e.Path)
	method := e.Method
	if method == "" {
		method = "GET"
	}
	if e.Body != "" && e.MultipartForm != nil {
		return nil, fmt.Errorf("Cannot use both 'body' and 'multipart_form'")
	}
	body := &bytes.Buffer{}
	var writer *multipart.Writer
	if e.Body != "" {
		body = bytes.NewBuffer([]byte(e.Body))
	} else if e.MultipartForm != nil {
		form, ok := e.MultipartForm.(map[interface{}]interface{})
		if !ok {
			return nil, fmt.Errorf("'multipart_form' should be a map")
		}
		writer = multipart.NewWriter(body)
		for keyf, valuef := range form {
			key, ok := keyf.(string)
			if !ok {
				return nil, fmt.Errorf("'multipart_form' should be a map with keys as strings")
			}
			value, ok := valuef.(string)
			if !ok {
				return nil, fmt.Errorf("'multipart_form' should be a map with values as strings")
			}
			// Considering file will be prefixed by @ (since you could also post regular data in the body)
			if strings.HasPrefix(value, "@") {
				// todo: how can we be sure the @ is not the value we wanted to use ?
				if _, err := os.Stat(value[1:]); !os.IsNotExist(err) {
					part, err := writer.CreateFormFile(key, filepath.Base(value[1:]))
					if err != nil {
						return nil, err
					}
					if err := writeFile(part, value[1:]); err != nil {
						return nil, err
					}
					continue
				}
			}
			if err := writer.WriteField(key, value); err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, path, body)
	if err != nil {
		return nil, err
	}
	if writer != nil {
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}
	return req, err
}

// writeFile writes the content of the file to an io.Writer
func writeFile(part io.Writer, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(part, file)
	return err
}
