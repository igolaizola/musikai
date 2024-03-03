# musikai

**musikai** is a tool to automatically generate music using AI.

## Commands

To see the list of available commands, run the following:

```bash
./musikai --help
```

To see the list of available options for a command, run the following:

```bash
./musikai {command} --help
```

### Generate

The `generate` command is used to generate songs.
You can specify the number of songs to generate, the account to use, the type of song, the prompt, and the style.
You should use either the prompt or the style, but not both.

```bash
./musikai generate --config generate.yaml
```

```yaml	
# generate.yaml
debug: false
db-type: postgres
db-conn: postgresql://musikai:password@provider-url:26257/musikai?sslmode=verify-full
s3-bucket: musikai
s3-region: eu-west-3
s3-key: s3-key
s3-secret: s3-secret
concurrency: 1
wait-min: 1s
wait-max: 2s
limit: 20
account: sunoaccoount
type: jazz
prompt: jazz
style: nostalgic mood ambient jazz
end-prompt: "[refrain]"
end-style: end # leave empty to use copy the song style
end-style-append: false # append the value instead of replacing it
min-duration: 2m5s
max-duration: 3m55s
max-extensions: 2
```

### Filter

The `filter` command is used to launch a web application to filter songs.

```bash
./musikai filter --config filter.yaml
```

```yaml
# filter.yaml
debug: false
db-type: postgres
db-conn: postgresql://musikai:password@provider-url:26257/musikai?sslmode=verify-full
port: 1337
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
db-type: postgres
db-conn: postgresql://musikai:password@provider-url:26257/musikai?sslmode=verify-full
```

## Setup

### Requirements

You need to have the following software installed and available in your PATH:

 - [ffmpeg](https://ffmpeg.org/)
 - [aubio](https://aubio.org/)
 - [phaselimiter](https://github.com/ai-mastering/phaselimiter)

### Sunno

You need to configure a Suno account to generate songs

You need to capture the cookie from Suno website.

1. Go to https://app.sunno.io/
2. Open the developer tools (F12)
3. Go to the "Network" tab
4. Refresh the page
5. Click on the first request to `https://clerk.suno.ai/v1/client?_clerk_js_version=4.70.1`
6. Go to the "Request Headers"
7. Copy the "cookie" header

Then you must store the cookie in your database.
Launch the following SQL command, replacing `cookievalue` with the value of the cookie you just copied and `accountname` with the name of your account.

```sql
INSERT INTO settings (id, created_at, updated_at, value)
VALUES ('suno/accountname/cookie', NOW(), NOW(), 'cookievalue');
```

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

### S3 storage

S3 storage is used to store the generated assets.

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

#### Make the bucket public

1. Go to the bucket you just created
2. Click on "Permissions" tab
3. Click on "Bucket Policy"
4. Add the following policy, replacing "musikai-s3" with your bucket name:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "PublicReadGetObject",
            "Effect": "Allow",
            "Principal": "*",
            "Action": "s3:GetObject",
            "Resource": "arn:aws:s3:::musikai-s3/*"
        }
    ]
}
```
