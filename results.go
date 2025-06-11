package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type Key struct {
	Region       string `json:"region"`
	Runtime      string `json:"runtime"`
	Architecture string `json:"architecture"`
}

type Result struct {
	Date    time.Time `json:"date"`
	Version string    `json:"version,omitempty"`
	Error   string    `json:"error,omitempty"`
}

type Results map[Key][]Result
type MarshalledResults map[string][]Result

func keyToString(k Key) string {
	return fmt.Sprintf("%s|%s|%s", k.Region, k.Runtime, k.Architecture)
}

func stringToKey(s string) Key {
	parts := strings.SplitN(s, "|", 3)
	return Key{
		Region:       parts[0],
		Runtime:      parts[1],
		Architecture: parts[2],
	}
}

func readJSON(filename string) (Results, error) {
	f, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return Results{}, nil
		}
		return nil, fmt.Errorf("failed to open JSON file: %w", err)
	}
	defer f.Close()
	var data MarshalledResults
	dec := json.NewDecoder(f)
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	var unmarshalled Results
	unmarshalled = make(Results)
	for k, v := range data {
		key := stringToKey(k)
		unmarshalled[key] = v
	}

	return unmarshalled, nil
}

func writeJSON(filename string, data Results) {
	marshalled := make(MarshalledResults)
	for k, v := range data {
		marshalled[keyToString(k)] = v
	}

	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("failed to create JSON file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(marshalled); err != nil {
		log.Fatalf("failed to encode JSON: %v", err)
	}
}
