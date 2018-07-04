package main

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	runewidth "github.com/mattn/go-runewidth"
)

// Forward slashes are one of the very few characters not allowed in paths
const delimiter string = "////"

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func failOn(b bool, message string) {
	if b {
		fail(errors.New(message))
	}
}

func keyval(line string) (string, string) {
	i := strings.Index(line, ":")
	if i == -1 || i == len(line)-1 {
		return line, ""
	}
	return line[:i], line[i+2:]
}

type Track struct {
	Album    string
	Artist   string
	Date     string
	Filename string
	Genre    string
	Path     string
	Time     string
	Title    string
}

func (t *Track) Set(key, value string) {
	switch key {
	case "Album":
		t.Album = value
	case "Artist":
		t.Artist = value
	case "Date":
		t.Date = value
	case "Genre":
		t.Genre = value
	case "Time":
		t.Time = formatDurationString(value)
	case "Title":
		t.Title = value
	}
}

func formatDurationString(str string) string {
	duration, err := time.ParseDuration(str + "s")
	if err != nil {
		return ""
	}
	zero := time.Time{}
	format := zero.Add(duration).Format("04:05")
	if duration > time.Hour {
		format = fmt.Sprintf("%d:%s", int(duration.Hours()), format)
	}
	return "(" + format + ")"
}

func withoutExt(path string) string {
	basename := filepath.Base(path)
	return strings.TrimSuffix(basename, filepath.Ext(basename))
}

func truncateAndPad(s string, maxWidth int, suffix string) string {
	if maxWidth < 0 {
		panic("suffix length greater than maxWidth chars")
	}
	return runewidth.FillRight(runewidth.Truncate(s, maxWidth, suffix), maxWidth)
}

func trackFormatter() func(*Track) string {
	var width, ignored int
	// tmux pane_width > $COLUMNS > stty size > default 80
	cmd := exec.Command("tmux", "display-message", "-p", "#{pane_width}")
	out, err := cmd.Output()
	if err == nil {
		_, err = fmt.Sscanf(string(out), "%d\n", &width)
	}

	if err != nil {
		width, err = strconv.Atoi(os.Getenv("COLUMNS"))
	}

	if err != nil {
		cmd := exec.Command("stty", "size")
		cmd.Stdin = os.Stdin
		out, err := cmd.Output()
		if err == nil {
			fmt.Sscanf(string(out), "%d %d\n", &ignored, &width)
		}
	}

	if width <= 20 {
		// A sane enough default/fallback
		width = 80
	}

	contentLen := width - 5 // remove 5 for fzf display
	return func(t *Track) string {
		str := t.Artist + " - " + t.Title
		str = strings.TrimPrefix(str, " - ")
		if str == "" {
			str = withoutExt(t.Filename)
		}
		if t.Album != "" {
			str += " {" + t.Album + "}"
		}
		str = truncateAndPad(str, contentLen-len(t.Time), "..")
		return str + t.Time + delimiter + t.Path
	}
}

func groupByArtist(tracks []*Track) []*Track {
	// group by artist, then shuffle to stop same order, but keep artist together
	artists := map[string][]*Track{}
	for _, t := range tracks {
		artists[t.Artist] = append(artists[t.Artist], t)
	}
	shuffled := make([]*Track, len(tracks))
	i := 0
	for _, tracks := range artists {
		for _, t := range tracks {
			shuffled[i] = t
			i += 1
		}
	}
	return shuffled
}

func parse(scan *bufio.Scanner) []*Track {
	tracks, track := []*Track{}, new(Track)
	// The old stack code didn't work as intended since it used slice operations
	// TODO -- Actually implement a stack that doesn't copy memory unnecessarily?
	// Probably isn't worth the complexity
	dirs := []string{}

	for scan.Scan() {
		key, value := keyval(scan.Text())
		switch key {
		case "directory":
			dirs = append(dirs, value)
		case "end":
			failOn(len(dirs) <= 0, "Invalid directory state. Corrupted database?")
			dirs = dirs[:len(dirs)-1]
		case "Artist", "Album", "Date", "Genre", "Time", "Title":
			track.Set(key, value)
		case "song_begin":
			track.Filename = value
			track.Path = filepath.Join(append(dirs, track.Filename)...)
		case "song_end":
			tracks = append(tracks, track)
			track = new(Track)
		}
	}
	fail(scan.Err())
	return tracks
}

func expandUser(path, home string) string {
	if path[:2] == "~/" {
		path = strings.Replace(path, "~", home, 1)
	}
	return path
}

func findDbFile() string {
	usr, err := user.Current()
	fail(err)
	home := usr.HomeDir
	paths := []string{
		filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "/mpd/mpd.conf"),
		filepath.Join(home, ".config", "/mpd/mpd.conf"),
		filepath.Join(home, ".mpdconf"),
		"/etc/mpd.conf",
		"/usr/local/etc/musicpd.conf",
	}
	var f *os.File
	var confpath string
	for _, path := range paths {
		f, err = os.Open(path)
		if err == nil {
			confpath = path
			break
		}
	}
	failOn(f == nil, "No config file found")

	expDb := regexp.MustCompile(`^\s*db_file\s*"([^"]+)"`)
	scan := bufio.NewScanner(f)
	var dbFile string
	for scan.Scan() {
		m := expDb.FindStringSubmatch(scan.Text())
		if m != nil {
			dbFile = expandUser(m[1], home)
		}
	}
	fail(scan.Err())
	fail(f.Close())
	failOn(dbFile == "", fmt.Sprintf("Could not find 'db_file' in configuration file '%s'", confpath))
	return dbFile
}

func fzfCheckExit(err error) {
	if err != nil {
		if exerr, ok := err.(*exec.ExitError); ok {
			if status, ok := exerr.Sys().(syscall.WaitStatus); ok {
				// FZF returns 130 when killed by ctrl+C
				if status.ExitStatus() == 130 {
					os.Exit(0)
				} else {
					fail(err)
				}
			} else {
				fail(err)
			}
		} else {
			fail(err)
		}
	}
}

func parseFzfOutput(output []byte) []string {
	songs := strings.Split(string(output), "\n")
	if len(songs) == 0 || songs[0] == "" {
		return []string{}
	}
	if songs[len(songs)-1] == "" {
		songs = songs[:len(songs)-1]
	}
	for i, s := range songs {
		songs[i] = s[strings.LastIndex(s, delimiter)+len(delimiter):]
	}

	return songs
}

func fzfSongs(tracks []*Track) []string {
	format := trackFormatter()
	fzf := exec.Command("fzf-tmux", "--no-hscroll", "-m")
	fzf.Stderr = os.Stderr

	in, err := fzf.StdinPipe()
	fail(err)
	out, err := fzf.StdoutPipe()
	fail(err)
	fail(fzf.Start())
	for _, t := range tracks {
		fmt.Fprintln(in, format(t))
	}
	fail(in.Close())
	fzfOutput, err := ioutil.ReadAll(out)
	fail(err)
	fzfCheckExit(fzf.Wait())

	return parseFzfOutput(fzfOutput)
}

func removeSongs(songs []string) error {
	fnames := make(map[string]struct{})
	for _, s := range songs {
		if s != "" {
			fnames[s] = struct{}{}
		}
	}
	mpc := exec.Command("mpc", "playlist", "-f", `%position% %file%`)
	out, err := mpc.Output()
	if err != nil {
		return err
	}

	mpc = exec.Command("mpc", "del")
	in, _ := mpc.StdinPipe()
	if err = mpc.Start(); err != nil {
		in.Close()
		return err
	}

	for _, s := range strings.Split(string(out), "\n") {
		posFname := strings.SplitN(s, " ", 2)
		if len(posFname) == 1 {
			continue
		}
		if _, ok := fnames[posFname[1]]; ok {
			fmt.Fprintln(in, posFname[0])
		}
	}

	if err = in.Close(); err != nil {
		return err
	}
	return mpc.Wait()
}

func insertSongs(songs []string) error {
	mpc := exec.Command("mpc", "insert")
	in, _ := mpc.StdinPipe()
	if err := mpc.Start(); err != nil {
		in.Close()
		return err
	}

	// Reverse order isn't required when adding a bunch of songs from stdin
	for _, s := range songs {
		fmt.Fprintln(in, s)
	}

	if err := in.Close(); err != nil {
		return err
	}
	return mpc.Wait()
}

func readTracks() []*Track {
	dbFile := findDbFile()

	f, err := os.Open(dbFile)
	fail(err)
	gz, err := gzip.NewReader(f)
	fail(err)

	scan := bufio.NewScanner(gz)
	tracks := groupByArtist(parse(scan))

	fail(gz.Close())
	fail(f.Close())
	return tracks
}

func main() {
	songs := fzfSongs(readTracks())
	if len(songs) == 0 {
		return
	}

	fail(removeSongs(songs))
	fail(insertSongs(songs))
}
