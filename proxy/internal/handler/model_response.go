package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const anthropicProtocolMessages = "anthropic_messages"

// restoreResponseModel keeps the client-visible model aligned with its request.
// Other response fields remain as reported by the upstream provider.
func restoreResponseModel(resp *http.Response, originalModel, routedModel, protocol string) error {
	if resp.StatusCode != http.StatusOK || originalModel == routedModel {
		return nil
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		upstreamBody := resp.Body
		reader, writer := io.Pipe()
		go func() {
			defer upstreamBody.Close()
			defer writer.Close()
			if err := rewriteSSEModel(writer, upstreamBody, originalModel, protocol); err != nil {
				_ = writer.CloseWithError(err)
			}
		}()
		resp.Body = reader
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
		return nil
	}

	upstreamBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	clientBody := rewriteModelJSON(upstreamBody, originalModel, protocol)
	resp.Body = io.NopCloser(bytes.NewReader(clientBody))
	resp.ContentLength = int64(len(clientBody))
	resp.Header.Set("Content-Length", strconv.Itoa(len(clientBody)))
	return nil
}

func rewriteModelJSON(body []byte, originalModel, protocol string) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return body
	}
	modelJSON, _ := json.Marshal(originalModel)
	var changed bool
	switch protocol {
	case anthropicProtocolMessages:
		if fields["type"] != nil && string(fields["type"]) == `"message_start"` {
			changed = rewriteNestedModel(fields, "message", modelJSON)
		} else if fields["model"] != nil {
			fields["model"] = modelJSON
			changed = true
		}
	case openAIProtocolChat:
		if fields["model"] != nil {
			fields["model"] = modelJSON
			changed = true
		}
	case openAIProtocolResponses:
		if fields["model"] != nil {
			fields["model"] = modelJSON
			changed = true
		}
		if rewriteNestedModel(fields, "response", modelJSON) {
			changed = true
		}
	}
	if !changed {
		return body
	}
	rewritten, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return rewritten
}

func rewriteNestedModel(fields map[string]json.RawMessage, key string, modelJSON []byte) bool {
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(fields[key], &nested); err != nil || nested["model"] == nil {
		return false
	}
	nested["model"] = modelJSON
	rewritten, err := json.Marshal(nested)
	if err != nil {
		return false
	}
	fields[key] = rewritten
	return true
}

func rewriteSSEModel(dst io.Writer, src io.Reader, originalModel, protocol string) error {
	reader := bufio.NewReader(src)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if bytes.HasPrefix(line, []byte("data:")) {
				content := bytes.TrimRight(line[len("data:"):], "\r\n")
				prefix := line[:len("data:")]
				space := content[:len(content)-len(bytes.TrimLeft(content, " \t"))]
				payload := bytes.TrimLeft(content, " \t")
				rewritten := rewriteModelJSON(payload, originalModel, protocol)
				if !bytes.Equal(rewritten, payload) {
					ending := []byte("\n")
					if bytes.HasSuffix(line, []byte("\r\n")) {
						ending = []byte("\r\n")
					} else if !bytes.HasSuffix(line, []byte("\n")) {
						ending = nil
					}
					line = append(append(append([]byte{}, prefix...), space...), rewritten...)
					line = append(line, ending...)
				}
			}
			if _, writeErr := dst.Write(line); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
