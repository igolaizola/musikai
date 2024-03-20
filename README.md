# musikai 🤖🎵

**musikai** is a tool to automatically generate and publish music using AI.

## 🔍 Overview

Musikai allows users to go from zero to publishing an album in digital music stores.

![](docs/musikai.excalidraw.png)

The steps to generate an album would be this:

1. **Generate**: use generate command to automatically generate hundreds of songs given a style or prompt.
2. **Process**: analyses the music to detect possible problems (long silences, unexpected BPM changes, etc), generating wave images, apply fade-outs and mastering the music.
3. **Filter songs**: Using the web app the user must approve the songs, with the help of the process data and the music itself. Every generated song has 2 endings, the best one being already selected, but it can also be changed.
4. **Song titles**: Song titles must be imported from an external csv or json file. The user can make them up or use external tools like ChatGPT.
5. **Drafts**: Album drafts with information such as title and optional subtitle and number of volumes must be also imported from an external csv or json file.
6. **Covers**: Cover generation takes titles from album drafts and uses midjourney to generate images.
7. **Upscale covers**: Once images are generated an upscale process uses Topaz Photo AI to upscale the images.
8. **Filter covers**: Using the web app the user must approve the covers.
9. **Album generation**: A command tool takes everything that has been generated and approved (songs, titles, drafts and covers) and generates albums.
10. **Filter albums**: Using the web app the user must approve the albums. Songs can be added or deleted during this process.
11. **Publish**: Albums are published to DistroKid automatically. In case you want to review before submitting, you can choose to press the button manually.

## 🕹️ Commands

To see the list of available commands, run the following:

```bash
./musikai --help
```

To see the list of available options for a command, run the following:

```bash
./musikai {command} --help
```

These help commands will show you always the latest options and commands available.
This README may be outdated, so it is recommended to use the help commands.

### Common options

The following options can be found in most commands:

#### `type` (string)

This value is used to classify songs. It can be a just style like `jazz`, an identifier like `jazz-test-01` or whatever you want. It is provided during the generation and then used to build the album or choose the DistroKid styles of the songs.

#### `db-type` (string) and `db-conn` (string)

- `db-type` (string): The type of the database. It can be `postgres`, `mysql`, or `sqlite`.
- `db-conn` (string): The connection string to the database. It must include the database name, the user, and the password. For sqlite, it must include the path to the sqlite file.

Example of a connection string for cockroachdb:

```
postgresql://my-username:my-password@my-cluster.cockroachlabs.cloud:26257/my-database?sslmode=verify-full
```

#### `fs-type` (string) and `fs-conn` (string)

- `fs-type` (string): The type of the file storage. It can be `telegram`, `s3` or `local`.
- `fs-conn` (string): The connection string to the file storage.
  - For telegram: `token@chat_id`
  - For s3: `key:secret@bucket.region`
  - For local: `/path/to/directory`

#### `proxy` (string)

The HTTP proxy to use. For example, `http://localhost:3128`.
This is optional.

#### `debug` (bool)

If set to true, the application will output debug information.

### Generate

The `generate` command is used to generate songs.
You can specify the number of songs to generate, the account to use, the type of song, the prompt, and the style.
You should use either the prompt or the style, but not both.

Suno generates first a fragment of around 2 minutes. 
Then you can extend this fragments multiple times.
There are some parameters to control how this extensions are done:
 - Duration of the song: `min-duration` and `max-duration` is used to continue extending or stop extending depending on the current total duration.
 - Number of extensions: `max-extensions` forces to end the generation once the maximum number of extensions is reached.
 - Final style and lyrics: In order to tell suno that you want to end the song you have to explicitly indicate it in the lyrics and/or style section.
   - Parameters `end-style`, `end-style-append` and `end-lyrics` are applied when minimum duration is reached and it is the first extension.
   - Parameters `force-end-style` and `force-end-lyrics` are applied when minimum duration is reached and it isn't the first extension.

```bash
./musikai generate --config generate.yaml
```

```yaml	
# generate.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
concurrency: 1
wait-min: 1s
wait-max: 2s
limit: 20
account: suno-account
type: jazz
prompt: jazz
style: nostalgic mood ambient jazz
end-lyrics: "[end]"
end-style: ". End." # leave empty to use copy the song style
end-style-append: true # append the value instead of replacing it
force-end-lyrics: "[end]"
force-end-style: short, end # leave empty to use copy the song style
min-duration: 2m5s
max-duration: 3m55s
max-extensions: 1
```

You can also use a csv/json file to use multiple prompts or styles.
The generator will choose randomly from the list of prompts or styles.
Weights can be used to make some prompts or styles more likely to be chosen.

```yaml
# generate.yaml
# same as before but without type, prompt, style
input: /path/to/file.csv
```

The file must have the following format:

```csv
weight,type,style,prompt,instrumental
10,jazz,nostalgic mood ambient jazz,,true
5,lofi,lofi chill,,true
```

### Process

The `process` command is used to post-process the songs.
These are the steps that are performed:
 - Detect if the song ends abruptly and apply a fade-out.
 - Detect if the song has long silences and flag it.
 - Detect if the song has unexpected BPM changes and flag it.
 - Mastering of the song.
 - Generate wave images.
 - Upload the processed songs and images to the file storage.

```bash
./musikai process --config process.yaml
```

```yaml
# process.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
concurrency: 1
short-fadeout: 1s
long-fadeout: 6s
```

### Web app

The `web` command is used to launch a web application to manage the songs, covers and albums.
This is used to manually approve or reject songs, covers and albums.

```bash
./musikai web --config web.yaml
```

```yaml
# web.yaml
debug: false
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
port: 1337
```

### Setting

The `setting` command is used to store settings such as the cookie for Suno or DistroKid.

```bash
./musikai setting --config setting.yaml
```

```yaml
# setting.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
service: suno/distrokid
account: accountname
value: cookievalue
```

### Title

The `title` command is used to import song titles from a csv or json file.

```bash
./musikai title --config title.yaml
```

```yaml
# title.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
input: /path/to/file.csv
```

### Draft

The `draft` command is used to import album drafts from a csv or json file.

```bash
./musikai draft --config draft.yaml
```

```yaml
# draft.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
input: /path/to/file.csv
```

The file must have the following fields:

 - Type is the classification of the songs.
 - Title is the main name of the album.
 - Subtitle is an additional text that is added in the cover of the album using font text.
 - Volumes is the maximum number of volumes that can be released with the same title and subtitle. The volume number is also added in the cover of the album using font text.

```csv
type,title,subtitle,volumes
jazz,Blue Moon Over The City,,
jazz,The Night We Met,,
```

### Cover

The `cover` command is used to generate covers for the albums.
Midjourney is used to generate the images.

```bash
./musikai cover --config cover.yaml
```

```yaml
# cover.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
template: Album cover. The album title "{TITLE}" should be in large, bold letters at the center, ensuring it's highly visible and legible. Don't add artist names.
input: /path/to/file.csv
minimum: 4 # minimum number of covers to generate
concurrency: 3
limit: 100 # total number of covers to generate
wait-min: 3s # minimum wait time between requests
wait-max: 5s # maximum wait time between requests
session: session.yaml # see how to configure midjourney session
```

The template can be any text that includes the `{title}` or `{TITLE}` (for uppercase) placeholders. The title will be replaced with the title of the album.

You can provide an input csv or json file with the map of which template to use for each type.

```csv
type,template
jazz,Jazz album cover with album title "{TITLE}".
edm,Electronic Dance Music album cover with album title "{TITLE}".
```

### Upscale

The `upscale` command is used to upscale the covers using Topaz Photo AI.

```bash
./musikai upscale --config upscale.yaml
```

```yaml
# upscale.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
concurrency: 1
upscale-type: topaz # realesrgan can also be used
upscale-bin: /path/to/topaz # optional, only needed if topaz is not in the default path
```

### Album

The `album` command is used to generate albums.

You can specify the minimum and maximum number of songs, the type of album, the artist, the font to use, the overlay to use and the genres to use.

```bash
./musikai album --config album.yaml
```

```yaml
# album.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
min-songs: 9
max-songs: 10
type: jazz
artist: Jazz-o-matic
font: fonts/Inter-Medium.ttf
overlay: overlays/jazz-o-matic.png
genres: genres.csv
```

The genres file must a json or csv file with the fields `type`, `primary`, and `secondary`. Secondary is optional.

```csv
type,primary,secondary
jazz,Jazz,Blues
edm,Electronic,Dance
rock,Rock,Alternative
lullaby,Children's Music,,
```

The genres must match the exactly one of the DistroKid genres.

```
Afrobeat
Afropop
Alternative
Big Band
Blues
Children's Music
Christian/Gospel
Classical
Comedy
Country
Dance
Electronic
Fitness & Workout
Folk
French Pop
German Folk
German Pop
Hip Hop/Rap
Holiday
J-Pop
Jazz
K-Pop
Latin
Latin Urban
Metal
New Age
Pop
Punk
R&B/Soul
Reggae
Rock
Singer/Songwriter
Soundtrack
Spoken Word
Vocal
World
```

#### Album cover

If you want to use a specific cover for the album, you can use the `cover-album`command to override the existing cover.

```bash
./musikai cover-album --config cover-album.yaml
```

```yaml
# cover-album.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
id: album-id
cover: /path/to/cover.jpg
```

#### Album delete

If you want to delete an album, you can use the `delete-album` command.
The songs and covers will be updated so they can be used in other albums.

```bash
./musikai delete-album --config delete-album.yaml
```

```yaml
# delete-album.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
id: album-id
```

### Publish

The `publish` command is used to publish the albums to DistroKid.
This command will open a browser window to DistroKid website and automatically fill the forms to publish the album.
If you set `auto` to true, the command will submit the album automatically.
If you set `auto` to false, the command will wait fill the forms and wait for you to click the submit button.

```bash
./musikai publish --config publish.yaml
```

```yaml
# publish.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
auto: true 
account: distrokid-account
first-name: John
last-name: Doe
record-label: Jazz-o-matic
type: jazz
```

### Download

The `download` command is used to download the songs from the file storage.
File names will be created using the database IDs.

```bash
./musikai download --config download.yaml
```

```yaml
# download.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
output: /path/to/output
```

#### Album download

The `album-download` command is used to download the album cover and songs from the file storage.
File names will be created using the album name and the song title.

```bash
./musikai album-download --config album-download.yaml
```

```yaml
# album-download.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
fs-type: local
fs-conn: /path/to/directory
output: /path/to/output
```

### Migrate

The `migrate` command is used to create the tables in the database.
Run this command once to create the tables.
Run this command again whenever you update the application to apply new migrations.

```bash
./musikai migrate --config migrate.yaml
```

```yaml
# migrate.yaml
debug: false
db-type: sqlite
db-conn: musikai.db
```

## 🛠️ Setup

### Requirements

You need to have the following software installed and available in your PATH:

#### ffmpeg

[ffmpeg.org](https://ffmpeg.org/)

You can use apt-get (linux), brew (mac) or scoop (windows) to install ffmpeg.

```bash
# linux
sudo apt-get install ffmpeg
# mac
brew install ffmpeg
# windows
scoop install ffmpeg
```

#### aubio

[aubio.org](https://aubio.org/)

You can use apt-get (linux), brew (mac) or scoop (windows) to install aubio.

```bash
# linux
sudo apt-get install aubio
# mac
brew install aubio
# windows
scoop install aubio
```

#### phaselimiter

[github.com/ai-mastering/phaselimiter](https://github.com/ai-mastering/phaselimiter)


To install phaselimiter on linux you can use this installation script: https://github.com/igolaizola/phaselimiter/blob/master/script/full-install-linux.sh

To install phaselimiter on mac you can use this installation script:
https://github.com/igolaizola/phaselimiter/blob/master/script/full-install-mac.sh
This won't work on mac with M1 or M2 chips.

For windows you can download these precompiled binaries: https://github.com/ai-mastering/phaselimiter/releases/download/v0.2.0/phaselimiter-win.zip

### Suno

You need to configure a Suno account to generate songs

You need to capture the cookie from Suno website.

1. Go to https://app.suno.ai/
2. Login if you are not already logged in
3. Open the developer tools (F12)
4. Go to the "Network" tab
5. Refresh the page
6. Click on the first request to `https://clerk.suno.ai/v1/client?_clerk_js_version=4.70.1`
7. Go to the "Request Headers"
8. Copy the "cookie" header

Then you must store the cookie in your database.
You can use the command `setting` to store the cookie in the settings table.
Pass the cookie as the value and choose a name for the account.

```bash
./musikai setting --config cookie.yaml
```

```yaml	
# cookie.yaml
debug: false
db-type: see common options
db-conn: see common options
service: suno
account: accountname
value: cookievalue
```

### DistroKid

You need to configure a DistroKid account to publish albums.

You need to capture the cookie from DistroKid website.

1. Go to https://distrokid.com/profile
2. Login if you are not already logged in
2. Open the developer tools (F12)
3. Go to the "Network" tab
4. Refresh the page
5. Click on the request to `https://distrokid.com/profile/`
6. Go to the "Request Headers"
7. Copy the "cookie" header

Then you must store the cookie in your database.
You can use the command `setting` to store the cookie in the settings table.
Pass the cookie as the value and choose a name for the account.

```bash
./musikai setting --config cookie.yaml
```

```yaml
# cookie.yaml
debug: false
db-type: see common options
db-conn: see common options
service: distrokid
account: accountname
value: cookievalue
```

### Midjourney

You need to configure a Midjourney account to generate covers.
Musikai uses [bulkai](https://github.com/igolaizola/bulkai) under the hood to generate the covers.

To generate a session file for midjourney, you need first to install bulkai and then run the following command:

```bash
go install github.com/igolaizola/bulkai/cmd/bulkai@latest
bulkai create-session
```

This will create a `session.yaml` file in the current directory.
You can use this file in the `cover` command.

### Database

Both postgres and mysql are supported.
SQLite is also supported but it is not recommended for production.

Once you have choosen your database provider you must create a database and a user with read/write access to the database.

```sql
CREATE DATABASE IF NOT EXISTS musikai;
CREATE USER IF NOT EXISTS musikai WITH PASSWORD 'P@ssw0rd!';
GRANT ALL ON DATABASE musikai TO musikai;
```

Your `db-type` setting must match your database provider (`postgres`, `mysql`, `sqlite`).
Your `db-conn` setting must match your database connection string, and must include the database name, the user, and the password.

Here is an example of a connection string for postgres:

```
postgresql://musikai:P@ssw0rd!@my-postgres-server.com:26257/musikai?sslmode=verify-full
```

Once you have created your database, you can use the `migrate` command to create the tables.


```bash
./musikai migrate --db-type {postgres,mysql,sqlite} --db-conn {connection-string,sqlite-file}
```

### File storage

#### Local storage

Local storage can be used to store the generated assets in any directory in your filesystem.
You can even use a google drive folder, a dropbox folder, or any other folder that is mounted in your filesystem.

```yaml
fs-type: local
fs-conn: /path/to/directory
```

#### Telegram storage

Telegram storage can used to store the generated assets in a telegram chat.
Use [t.me/botfather](https://t.me/botfather) to create a bot and obtain the token.
Then create a chat with the bot and obtain the chat id.
You can obtain the chat id by opening the chat on web.telegram.org and looking at the URL. The chat id is the number after `https://web.telegram.org/a/#`.

```yaml
fs-type: telegram
fs-conn: token@chat_id
```

#### S3 storage

S3 storage can be used to store the generated assets in an AWS S3 bucket.

```yaml
fs-type: s3
fs-conn: key:secret@bucket.region
```

Here is a guide to create a bucket on AWS and obtain credentials.

#### Create a bucket

1. Go to Buckets: https://s3.console.aws.amazon.com/s3/buckets
2. Click Create bucket
3. Choose a name and region. For example, "musikai-s3" and "Europe (Frankfurt) eu-central-1"
4. Click Create bucket

#### Obtain credentials

1. Go to "Identity and Access Management (IAM)": https://eu-central-1.console.aws.amazon.com/iamv2
2. Select "Users" from "Access Management"
3. Click "Add users"
4. Choose a name and click Next. For example, "musikai-s3-service"
5. Select "Attach policies directly" and choose "AmazonS3FullAccess"
6. Click Next and then Create user
7. Click on the user name you just created
8. Click on "Security credentials" tab
9. Click on "Create access key"
10. Select "Application running outside AWS".
11. Add a description and click "Create access key". For example "musikai-s3-service-key"
12. Copy the "Access key ID" and "Secret access key" and save them in a safe place.

## ⚠️ Disclaimer

The automation of User Discord accounts also known as self-bots is a violation of Discord Terms of Service & Community guidelines and will result in your account(s) being terminated.

The automation of Midjourney and Suno accounts is also a violation of their Terms of Service and will result in your account(s) being terminated.

Read about Discord, Midjourney and Suno Terms of Service and Community Guidelines

The authors are released of any liabilities which your usage may entail.
