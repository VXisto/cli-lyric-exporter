package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/schollz/progressbar/v3"
)

type Song struct {
	Title  string
	URL    string
	Lyrics string
	Error  error
}

type Scraper struct {
	baseURL      string
	workerCount  int
	debug        bool
	logger       *log.Logger
	client       *http.Client
	maxRetries   int
	retryBackoff time.Duration
}

type ScraperConfig struct {
	WorkerCount  int
	Debug        bool
	MaxRetries   int
	RetryBackoff time.Duration
}

func NewScraper(config ScraperConfig) *Scraper {
	return &Scraper{
		baseURL:      "https://letras.mus.br",
		workerCount:  config.WorkerCount,
		debug:        config.Debug,
		logger:       log.New(os.Stdout, "[SCRAPER] ", log.Ltime),
		maxRetries:   config.MaxRetries,
		retryBackoff: config.RetryBackoff,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *Scraper) debugLog(format string, v ...interface{}) {
	if s.debug {
		s.logger.Printf(format, v...)
	}
}

func (s *Scraper) retryOperation(ctx context.Context, operation string, fn func() error) error {
	var err error
	backoff := s.retryBackoff

	for retry := 0; retry <= s.maxRetries; retry++ {
		if retry > 0 {
			s.debugLog("Retrying %s (attempt %d/%d) after %v", operation, retry, s.maxRetries, backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2 // Exponential backoff
		}

		if err = fn(); err == nil {
			return nil
		}

		s.debugLog("Error in %s (attempt %d/%d): %v", operation, retry+1, s.maxRetries, err)
	}

	return fmt.Errorf("failed after %d retries: %w", s.maxRetries, err)
}

func (s *Scraper) getSongList(ctx context.Context, artist string) ([]Song, error) {
	url := fmt.Sprintf("%s/%s", s.baseURL, artist)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch artist page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var songs []Song
	// Update the selector to handle both possible HTML structures
	selectors := []string{
		".cnt-artist-songlist.artista-todas a",
		".songList-table-row.--song a", // Alternative selector
		".artista-todas a",             // Fallback selector
	}

	for _, selector := range selectors {
		doc.Find(selector).Each(func(i int, sel *goquery.Selection) {
			href, exists := sel.Attr("href")
			if exists {
				title := strings.TrimSpace(sel.Text())
				if title != "" { // Only add if we have a title
					songs = append(songs, Song{
						Title: title,
						URL:   fmt.Sprintf("%s%s", s.baseURL, href),
					})
				}
			}
		})

		// If we found any songs, break the loop
		if len(songs) > 0 {
			break
		}
	}

	if len(songs) == 0 {
		return nil, fmt.Errorf("no songs found for artist %s", artist)
	}

	// Remove duplicates
	uniqueSongs := make([]Song, 0, len(songs))
	seen := make(map[string]bool)

	for _, song := range songs {
		if !seen[song.URL] {
			seen[song.URL] = true
			uniqueSongs = append(uniqueSongs, song)
		}
	}

	return uniqueSongs, nil
}

func (s *Scraper) getLyrics(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch lyrics page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Try multiple possible selectors for lyrics
	selectors := []string{
		".lyric-original",
		".cnt-letra", // Alternative selector
		".letra",     // Fallback selector
	}

	var lyrics string
	for _, selector := range selectors {
		lyrics = strings.TrimSpace(doc.Find(selector).Text())
		if lyrics != "" {
			break
		}
	}

	if lyrics == "" {
		return "", fmt.Errorf("no lyrics found on page")
	}

	return lyrics, nil
}

func (s *Scraper) formatLyrics(lyrics string) string {
	// Replace multiple spaces with single space
	lyrics = strings.Join(strings.Fields(lyrics), " ")

	// Add proper line breaks
	lyrics = strings.ReplaceAll(lyrics, ". ", ".\n")
	lyrics = strings.ReplaceAll(lyrics, "! ", "!\n")
	lyrics = strings.ReplaceAll(lyrics, "? ", "?\n")

	// Handle common verse separators
	lyrics = strings.ReplaceAll(lyrics, "\n\n\n", "\n\n")

	return lyrics
}

func (s *Scraper) saveLyrics(artist string, song Song) error {
	if song.Lyrics == "" {
		return fmt.Errorf("no lyrics to save")
	}

	formattedLyrics := s.formatLyrics(song.Lyrics)
	content := fmt.Sprintf("Title: %s\nArtist: %s\n\n%s\n", song.Title, artist, formattedLyrics)

	filename := fmt.Sprintf("lyrics/%s/%s.txt", artist, sanitizeFilename(song.Title))
	return os.WriteFile(filename, []byte(content), 0644)
}

func (s *Scraper) saveAllLyrics(artist string, songs []Song) error {
	if len(songs) == 0 {
		return fmt.Errorf("no songs to save")
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("Artist: %s\n", artist))
	content.WriteString(fmt.Sprintf("Number of songs: %d\n", len(songs)))
	content.WriteString("===================\n\n")

	for _, song := range songs {
		content.WriteString(fmt.Sprintf("### %s ###\n\n", song.Title))
		content.WriteString(s.formatLyrics(song.Lyrics))
		content.WriteString("\n\n===================\n\n")
	}

	filename := fmt.Sprintf("lyrics/%s/all_lyrics.txt", artist)
	return os.WriteFile(filename, []byte(content.String()), 0644)
}

func (s *Scraper) saveLLMFormat(artist string, songs []Song) error {
	if len(songs) == 0 {
		return fmt.Errorf("no songs to save")
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("Collection of lyrics by %s\n", artist))
	content.WriteString("Format: Each song is marked with [SONG] and [END] tags\n\n")

	for _, song := range songs {
		content.WriteString(fmt.Sprintf("[SONG:%s]\n", song.Title))
		content.WriteString(s.formatLyrics(song.Lyrics))
		content.WriteString("\n[END]\n\n")
	}

	filename := fmt.Sprintf("lyrics/%s/%s_llm_format.txt", artist, sanitizeFilename(artist))
	return os.WriteFile(filename, []byte(content.String()), 0644)
}

func (s *Scraper) ProcessArtist(ctx context.Context, artist string) error {
	outputDir := fmt.Sprintf("lyrics/%s", artist)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	s.debugLog("Starting to fetch song list for artist: %s", artist)
	songs, err := s.getSongList(ctx, artist)
	if err != nil {
		return fmt.Errorf("failed to get song list: %w", err)
	}
	s.debugLog("Found %d songs for artist %s", len(songs), artist)

	// Initialize progress bar
	bar := progressbar.NewOptions(len(songs),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetDescription("[cyan][1/2][reset] Downloading lyrics..."),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	results := make(chan Song, len(songs))
	var wg sync.WaitGroup
	jobs := make(chan Song, len(songs))

	// Start workers
	for i := 0; i < s.workerCount; i++ {
		wg.Add(1)
		go s.worker(ctx, i, jobs, results, &wg, bar)
	}

	// Send jobs to workers
	go func() {
		for _, song := range songs {
			select {
			case jobs <- song:
				s.debugLog("Queued song: %s", song.Title)
			case <-ctx.Done():
				return
			}
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var processedSongs []Song
	var failedSongs []Song

	// Initialize saving progress bar
	saveBar := progressbar.NewOptions(len(songs),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetDescription("[cyan][2/2][reset] Saving files..."),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	for song := range results {
		if song.Error != nil {
			s.logger.Printf("Error processing %s: %v", song.Title, song.Error)
			failedSongs = append(failedSongs, song)
			saveBar.Add(1)
			continue
		}

		err := s.retryOperation(ctx, fmt.Sprintf("saving %s", song.Title), func() error {
			return s.saveLyrics(artist, song)
		})

		if err != nil {
			s.logger.Printf("Error saving %s: %v", song.Title, err)
			failedSongs = append(failedSongs, song)
		} else {
			processedSongs = append(processedSongs, song)
			s.debugLog("Successfully processed: %s", song.Title)
		}
		saveBar.Add(1)
	}

	fmt.Println() // New line after progress bars

	if err := s.saveAllLyrics(artist, processedSongs); err != nil {
		return fmt.Errorf("failed to save combined lyrics: %w", err)
	}

	// Ask user if they want to save in LLM format
	var response string
	fmt.Print("\nWould you like to save all lyrics in a single file optimized for LLM ingestion? (y/N): ")
	fmt.Scanln(&response)

	if strings.ToLower(response) == "y" || strings.ToLower(response) == "yes" {
		if err := s.saveLLMFormat(artist, processedSongs); err != nil {
			s.logger.Printf("Warning: Failed to save LLM format: %v", err)
		} else {
			s.logger.Printf("Successfully saved LLM format file")
		}
	}

	s.logger.Printf("\nScraping completed:")
	s.logger.Printf("- Total songs: %d", len(songs))
	s.logger.Printf("- Successfully processed: %d", len(processedSongs))
	s.logger.Printf("- Failed: %d", len(failedSongs))

	if len(failedSongs) > 0 {
		s.logger.Println("\nFailed songs:")
		for _, song := range failedSongs {
			s.logger.Printf("- %s: %v", song.Title, song.Error)
		}
	}

	return nil
}

func (s *Scraper) worker(ctx context.Context, id int, jobs <-chan Song, results chan<- Song, wg *sync.WaitGroup, bar *progressbar.ProgressBar) {
	defer wg.Done()

	for song := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
			s.debugLog("Worker %d processing: %s", id, song.Title)

			err := s.retryOperation(ctx, fmt.Sprintf("downloading %s", song.Title), func() error {
				lyrics, err := s.getLyrics(ctx, song.URL)
				if err != nil {
					return err
				}
				song.Lyrics = lyrics
				return nil
			})

			if err != nil {
				song.Error = err
			}

			results <- song
			bar.Add(1)

			// Be polite to the server
			time.Sleep(time.Second)
		}
	}
}

func sanitizeFilename(filename string) string {
	invalid := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := filename

	for _, char := range invalid {
		result = strings.ReplaceAll(result, char, "_")
	}

	return strings.TrimSpace(result)
}

func main() {
	// Command line flags
	workerCount := flag.Int("workers", 5, "Number of concurrent workers")
	debug := flag.Bool("debug", false, "Enable debug logging")
	maxRetries := flag.Int("retries", 3, "Maximum number of retries per request")
	retryBackoff := flag.Duration("backoff", 2*time.Second, "Initial retry backoff duration")
	flag.Parse()

	config := ScraperConfig{
		WorkerCount:  *workerCount,
		Debug:        *debug,
		MaxRetries:   *maxRetries,
		RetryBackoff: *retryBackoff,
	}

	scraper := NewScraper(config)

	var artist string
	fmt.Print("Enter artist name (as it appears in the URL): ")
	fmt.Scanln(&artist)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := scraper.ProcessArtist(ctx, artist); err != nil {
		log.Fatal(err)
	}
}
