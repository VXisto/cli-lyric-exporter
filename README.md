# CLI Lyric Exporter

A command-line tool written in Go that exports lyrics from Letras.mus.br. This tool efficiently scrapes and downloads lyrics for all songs from a specified artist, saving them both as individual files and in combined formats.

## Features

- Concurrent downloading with configurable number of workers
- Robust error handling with retries
- Progress bars for download and save operations
- Multiple output formats:
  - Individual text files for each song
  - Combined file with all lyrics
  - LLM-optimized format (optional)
- Polite scraping with rate limiting
- Debug logging option

## Installation

1. Make sure you have Go installed on your system
2. Clone the repository:
```bash
git clone https://github.com/yourusername/cli-lyric-exporter.git
cd cli-lyric-exporter
```
3. Install dependencies:
```bash
go mod download
```

## Usage

Run the program with:

```bash
go run main.go [flags]
```

### Available Flags

- `-workers`: Number of concurrent workers (default: 5)
- `-debug`: Enable debug logging (default: false)
- `-retries`: Maximum number of retries per request (default: 3)
- `-backoff`: Initial retry backoff duration (default: 2s)

### Example

```bash
go run main.go -workers 3 -debug
```

When prompted, enter the artist name as it appears in the Letras.mus.br URL. For example, for `https://letras.mus.br/fresno/`, enter `fresno`.

## Output Structure

```
lyrics/
└── artist_name/
    ├── song1.txt
    ├── song2.txt
    ├── ...
    ├── all_lyrics.txt
    └── artist_name_llm_format.txt (optional)
```

### Output Formats

1. Individual song files (`song.txt`):
```
Title: Song Name
Artist: Artist Name

[Lyrics with proper formatting]
```

2. Combined file (`all_lyrics.txt`):
```
Artist: Artist Name
Number of songs: X
===================

### Song 1 ###
[Lyrics]

===================

### Song 2 ###
[Lyrics]

===================
```

3. LLM format (`artist_name_llm_format.txt`):
```
Collection of lyrics by Artist Name
Format: Each song is marked with [SONG] and [END] tags

[SONG:Song 1]
[Lyrics]
[END]

[SONG:Song 2]
[Lyrics]
[END]
```

## Error Handling

- The tool automatically retries failed requests
- Failed songs are logged with their respective errors
- A summary is provided after completion showing:
  - Total songs processed
  - Successfully downloaded songs
  - Failed songs with reasons

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

