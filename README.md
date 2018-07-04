# mpd-fzf

mpd-fzf is a minimal [Music Player Daemon][mpd] (mpd) track selector that makes it easy to enqueue tracks after the current track.

mpd-fzf parses the mpd database and passes a list of tracks to the [fzf][fzf] command-line finder. This offers a fast way to explore a music collection interactively.

Tracks are formatted as "Artist - Track {Album} (MM:SS)", defaulting to the filename if there's insufficient information.

## Installation

    $ go get -u github.com/awused/mpd-fzf

It also requires that [MPC][mpc] is installed and properly configured (using `$MPD_HOST` if necessary).

    $ sudo apt-get install mpc

or

    $ sudo pacman -S mpc

## Usage

Running `mpd-fzf` will send the entire mpd database to fzf. Select multiple tracks with TAB or simply hit enter for a single track. Tracks will be added in order after the currently playing track.

It can also be used as a tmux shortcut `bind-key m run "mpd-fzf"`.

## Changes From aver-d/mpd-fzf

### Functionality

The biggest change is the behavioural change. Instead of staying open and playing a new track every time enter is pushed, it takes the output from FZF and adds them after the currently playing track then exits.

* Uses fzf-tmux, which will gracefully fall back regular fzf if tmux isn't running
* Reads pane width from tmux if possible instead of using stty width
* Uses fzf -m to allow multiple selections with TAB
* Doesn't require an external script
* Performance when parsing the database and building FZF's input is improved
* Works on FreeBSD

### Bug Fixes

* Handles filenames containing exclamation points properly, which are improperly escaped by FZF when using --bind
* Handles wide characters in tracks
* Uses a delimiter that cannot be found in file names

____

License: MIT

[mpd]: https://www.musicpd.org
[mpc]: https://www.musicpd.org/clients/mpc
[fzf]: https://github.com/junegunn/fzf

