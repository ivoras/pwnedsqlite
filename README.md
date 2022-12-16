# pwnedsqlite
A Go tool which imports the Have I Been Pwned file of password hashes into a SQLite database on the fly, without decompressing it first.

Usage:

```
Usage of ./pwnedsqlite:
  -f    Delete old SQLite output file before ingesting new data (default true)
  -i string
        Input file name (7zip file from https://haveibeenpwned.com/Passwords)
  -o string
        Output filename SQLite database (default "pwned.sqlite")
```
