package wip

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	checkpointNamespace = "go-git-wip:"
	checkpointMarkerV1  = "go-git-wip:1 "
	checkpointMarkerV2  = "go-git-wip:2 "
	checkpointEscape    = "Checkpoint: "
)

type checkpointMetadata struct {
	Branch       string `json:"branch"`
	Base         string `json:"base"`
	MessageBytes int    `json:"message_bytes"`
}

func encodeCheckpointMessage(branch, base, message string) ([]byte, error) {
	metadata, err := json.Marshal(checkpointMetadata{Branch: branch, Base: base, MessageBytes: len(message)})
	if err != nil {
		return nil, err
	}
	display := message
	if strings.HasPrefix(display, checkpointNamespace) {
		display = checkpointEscape + display
	}
	return []byte(display + "\n\n" + checkpointMarkerV2 + string(metadata) + "\n"), nil
}

// Leading namespace dispatch keeps legacy caller text independent of footer text.
// Version 2 reads only the final line and uses byte length to preserve whitespace.
func decodeCheckpointMessage(body string) (checkpointMetadata, string, error) {
	if strings.ContainsRune(body, 0) {
		return checkpointMetadata{}, "", fmt.Errorf("checkpoint message contains NUL")
	}
	if strings.HasPrefix(body, checkpointNamespace) {
		line, message, found := strings.Cut(body, "\n\n")
		if !found || !strings.HasPrefix(line, checkpointMarkerV1) || strings.ContainsRune(line, '\n') {
			return checkpointMetadata{}, "", fmt.Errorf("commit is not a version 1 checkpoint")
		}
		metadata, err := decodeCheckpointMetadata(strings.TrimPrefix(line, checkpointMarkerV1), 1)
		return metadata, strings.TrimSuffix(message, "\n"), err
	}
	if !strings.HasSuffix(body, "\n") {
		return checkpointMetadata{}, "", fmt.Errorf("checkpoint footer must end with one newline")
	}
	withoutFinalNewline := strings.TrimSuffix(body, "\n")
	separator := strings.LastIndexByte(withoutFinalNewline, '\n')
	if separator < 1 || withoutFinalNewline[separator-1] != '\n' {
		return checkpointMetadata{}, "", fmt.Errorf("checkpoint footer requires a blank-line separator")
	}
	line := withoutFinalNewline[separator+1:]
	if !strings.HasPrefix(line, checkpointMarkerV2) {
		return checkpointMetadata{}, "", fmt.Errorf("commit is not a version 2 checkpoint")
	}
	metadata, err := decodeCheckpointMetadata(strings.TrimPrefix(line, checkpointMarkerV2), 2)
	if err != nil {
		return checkpointMetadata{}, "", err
	}
	display := withoutFinalNewline[:separator-1]
	if len(display) == metadata.MessageBytes && !strings.HasPrefix(display, checkpointNamespace) {
		return metadata, display, nil
	}
	if strings.HasPrefix(display, checkpointEscape) {
		message := strings.TrimPrefix(display, checkpointEscape)
		if len(message) == metadata.MessageBytes && strings.HasPrefix(message, checkpointNamespace) {
			return metadata, message, nil
		}
	}
	return checkpointMetadata{}, "", fmt.Errorf("checkpoint message byte count or display framing is invalid")
}

// Decode field tokens explicitly so null, duplicates, and numeric coercion fail.
func decodeCheckpointMetadata(raw string, version int) (checkpointMetadata, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return checkpointMetadata{}, fmt.Errorf("checkpoint metadata must be a JSON object")
	}
	var metadata checkpointMetadata
	seen := make(map[string]bool, 3)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return checkpointMetadata{}, err
		}
		key, ok := token.(string)
		if !ok || (key != "branch" && key != "base" && !(version == 2 && key == "message_bytes")) {
			return checkpointMetadata{}, fmt.Errorf("unknown checkpoint metadata field")
		}
		if seen[key] {
			return checkpointMetadata{}, fmt.Errorf("duplicate checkpoint metadata field")
		}
		seen[key] = true
		value, err := decoder.Token()
		if err != nil {
			return checkpointMetadata{}, err
		}
		if key == "message_bytes" {
			number, ok := value.(json.Number)
			if !ok {
				return checkpointMetadata{}, fmt.Errorf("message_bytes must be a nonnegative integer")
			}
			metadata.MessageBytes, err = strconv.Atoi(string(number))
			if err != nil || metadata.MessageBytes < 0 {
				return checkpointMetadata{}, fmt.Errorf("message_bytes must be a nonnegative integer")
			}
			continue
		}
		text, ok := value.(string)
		if !ok {
			return checkpointMetadata{}, fmt.Errorf("branch and base must be strings")
		}
		if key == "branch" {
			metadata.Branch = text
		} else {
			metadata.Base = text
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return checkpointMetadata{}, fmt.Errorf("invalid checkpoint metadata object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return checkpointMetadata{}, fmt.Errorf("trailing checkpoint metadata data")
	}
	if metadata.Branch == "" || metadata.Base == "" || (version == 2 && !seen["message_bytes"]) {
		return checkpointMetadata{}, fmt.Errorf("required checkpoint metadata field missing or empty")
	}
	return metadata, nil
}
