package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func main() {
	mem0URL := flag.String("url", "http://localhost:9091", "Mem0 service URL")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: proxy-memory <command> [args]")
		fmt.Fprintln(os.Stderr, "Commands: search <query>, recent, export")
		os.Exit(1)
	}

	switch args[0] {
	case "search":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: proxy-memory search <query>")
			os.Exit(1)
		}
		searchMemories(*mem0URL, strings.Join(args[1:], " "))
	case "recent":
		listRecent(*mem0URL)
	case "export":
		exportMemories(*mem0URL)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		os.Exit(1)
	}
}

func searchMemories(url, query string) {
	payload := map[string]interface{}{
		"query": query,
		"limit": 10,
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url+"/v1/memories/search/", "application/json", strings.NewReader(string(body)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var results []struct {
		ID     string  `json:"id"`
		Memory string  `json:"memory"`
		Score  float64 `json:"score"`
	}
	json.NewDecoder(resp.Body).Decode(&results)

	for _, r := range results {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Memory)
	}
}

func listRecent(url string) {
	resp, err := http.Get(fmt.Sprintf("%s/v1/memories/?limit=20", url))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var results []struct {
		ID        string `json:"id"`
		Memory    string `json:"memory"`
		CreatedAt string `json:"created_at"`
	}
	json.NewDecoder(resp.Body).Decode(&results)

	for i, r := range results {
		fmt.Printf("%d. [%s] %s\n", i+1, r.CreatedAt, r.Memory)
	}
}

func exportMemories(url string) {
	resp, err := http.Get(fmt.Sprintf("%s/v1/memories/?limit=1000", url))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var results []struct {
		ID        string `json:"id"`
		Memory    string `json:"memory"`
		CreatedAt string `json:"created_at"`
	}
	json.NewDecoder(resp.Body).Decode(&results)

	for _, r := range results {
		json.NewEncoder(os.Stdout).Encode(r)
	}
}
