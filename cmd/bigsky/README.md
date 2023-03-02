
bigsky: A Big Graph Server (BGS)
================================

## Database Setup

PostgreSQL and Sqlite are both supported. When using Sqlite, separate database
for the BGS database itself and the CarStore are used. With PostgreSQL a single
database server, user, and database, can all be reused.

Database configuration is passed via the `DATABASE_URL` and
`CARSTORE_DATABASE_URL` environment variables, or the corresponding CLI args.

For PostgreSQL, the user and database must already be configured. Some example
SQL commands are:

    CREATE DATABASE bgs;
    CREATE DATABASE carstore;

    CREATE USER ${username} WITH PASSWORD '${password}';
    GRANT ALL PRIVILEGES ON DATABASE bgs TO ${username};
    GRANT ALL PRIVILEGES ON DATABASE carstore TO ${username};

This service currently uses `gorm` to automatically run database migrations as
the regular user. There is no concept of running a separate set of migrations
under more privileged database user.
