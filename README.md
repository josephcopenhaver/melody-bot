# melody-bot

My discord bot

## build steps:

```sh
mage shell # can prefix with "NOBUILD=y" to avoid rebuilding the shell container
mage build
```

## configure stack:

to create/edit a environment file for ENV=test:

```sh
code ./secrets/test.env
```

## create stack (with new build):

```sh
mage up
```

## create stack (with old build):

```sh
NOBUILD=y mage up
```

## teardown stack

```sh
mage down
```

## cleanup build output and volumes

```sh
mage clean
```

## required bot permissions

- View Channels
- Send Messages
- Send TTS Messages ( not used yet )
- Embed Links ( not used yet )
- Connect
- Speak

## supported bot commands

output of "@bot help" in a guild channel or "help" when talking to the bot in a DM:

```yaml
---
#
# help:
#

cache-url:
  usage: cache <url>
  description: process music from a video url for playing at a future time

clear cache:
  usage: clear cache
  description: stops all players and clears files in the audio cache

clear-playlist:
  usage: clear playlist
  description: removes all tracks in the playlist: alias for reset

echo:
  usage: echo <message>
  description: responds with the same message provided

help:
  usage: help
  description: enumerates each bot command, it's syntax, and what the command does

join-channel:
  usage: join <channel_name>
  description: makes the bot join a specific voice channel

next:
  usage: <next|skip>
  description: move playback to the next track in the playlist

pause:
  usage: pause
  description: pauses playback and remember position in the current track; can be resumed

ping:
  usage: ping
  description: responds with pong message

play:
  usage: play <url>
  description: append track from youtube url to the playlist

previous:
  usage: <previous|prev>
  description: move playback to the previous track in the playlist

remove-track:
  usage: remove <track_url>
  description: removes a track from the playlist

repeat:
  usage: repeat
  description: cycles playlist repeat mode between ["repeating", "not repeating"]

reset:
  usage: reset
  description: resets player state back to defaults: stops playback and clears the playlist

restart-track:
  usage: restart track
  description: if playback is in the middle of a track, rewind to the start of the track

resume:
  usage: <resume|unpause|play>
  description: if stopped or paused, resumes playback

set-text-channel:
  usage: set text channel
  description: bot sends system text messages to the guild channel that this command is issued from

show-playlist:
  usage: show playlist
  description: prints the current playlist

stop:
  usage: stop
  description: stops playback of current track and rewinds to the beginning of the current track
```
