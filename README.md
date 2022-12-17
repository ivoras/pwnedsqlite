# How and why?

Apparently because doing mini projects like this relaxes me. 

I was reading through the [Has your password been pwned? Or, how I almost failed to search a 37 GB text file in under 1 millisecond (in Python)](https://death.andgravity.com/pwned) article and several armchair engineer thoughs came to me:

* Allright, why is the guy doing this in Python?
* Why is the source dataset in a weird type of CSV - not comma-separated-value files, but colon-separated-value?
* Why aren't datasets like these distributed in a form which can readily be queried and analysed and transformed, and not just sit there for idle hackers to write file-based binary searches for them?

Of course, all those are tongue in cheek.

## Modern computers are fast

So fast, in fact, that I don't think it can be appreciated by people who have not used them when they were really slow. I did BASIC on an ATARI machine I forgot the model name of, and I did QBASIC on a 8088, and I didn't complain about the speed back then because I had no alternative. It led me to a path where I still ocasionally dissassemble a loop I did in a compiled language just to verify the compiler is doing its job reasonably well, and where I really do enjoy things like kernel programming, and writing firmware for microcontrollers. We all have our fun in life.

So, OPs goal was to achive sub 1 ms query on this data set, without modifying the file format. And - spoilers - he did it, and wrote a very enjoyable article while doing so. I respect those kinds of microoptimisations of critical paths.

But you know what would be even more awesome? Doing it with a proper file format.

Sure, I could make my own B-tree engine, but why bother? There is an universal database format out there which everyone sane can use nowadays: SQLite. The sure-fire way to make the original performance goal must be to create an indexed SQLite table.

# Importing data into SQLite

Since we're going to use SQLite's query engine, the interesting bits here are getting data into SQLite.

The schema is simple:

```sql
CREATE TABLE hashes (hash TEXT, count INTEGER);
CREATE INDEX hashesh_hash ON hashes(hash);"
```

And the resulting SQLite database's size is 87 GB in size (847100000 records). That's over twice the size of the .txt file.

## Decompressing on the fly

For some reason, I thought it a bit dirty to decompress the orginal file first, so I found a 7zip decompression library (`github.com/bodgit/sevenzip`) and used that to read data directly from the compressed archive. It has a really nice interface where the code to get a `ReaderCloser` the `.txt` file boiled down to:

```go
        r, err := sevenzip.OpenReader(*inFilename)
        if err != nil {
                fmt.Println(err)
                return
        }
        for _, file := range r.File {
                fmt.Println(file.Name)
                if strings.HasSuffix(file.Name, ".txt") {
                        rc, err := file.Open()
                        if err != nil {
                                fmt.Println(err)
                                return
                        }
                        err = ingestData(db, rc)
                        rc.Close()
                        if err != nil {
                                fmt.Println(err)
                        }
                }
        }
```

I made a generous buffered Reader in the `ingestData` function:

```go
        rd := bufio.NewReaderSize(rc, 4*1024*1024)
```

With the reasoning being that 4 MiB + temporary structures will fit in L3 caches even in older CPUs, and decompressing data in bulk should be very efficient in any case.

The next bit was interesting: how to parse the lines. Originally, this was supposed to be single-threaded code, so I tried my best to use the usual optimisations: avoding memcpys and mallocs. Originally, the beginning the read loop looked like this:

```go
	for {
		line, err := rd.ReadSlice(byte('\n'))
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		hash := line[0:40]
		countString := line[41 : len(line)-2]
		countInt, err := strconv.Atoi(string(countString))
		if err != nil {
			return err
		}
```

The `ReadSlice()` function is interesting as it makes uses of Go's slice arithmetic, which in this case looks like what I'd do with pointer arithmetic in C. At each call, it returns a pointer into an existing buffer (the 4 MiB one we allocated with the BufferedReader), avoding allocations and copying of data. Since this option exists, I was very annoyed at having to make a malloc+memcpy for converting the `[]byte` slice to string just to feed it to Atoi. I was seriously contemplating making my own `atoi()` which would work on a byte slice, simply for the elegance of not having an explosion of tiny strings in a tight loop for the GC to collect.

The next thing on the menu was getting data into SQLite. I've used the [wrapped C version from mattn](https://github.com/mattn/go-sqlite3). Here I knew two things: that it pays of big time (performance-wise) to create large-ish transactions which insert data in bulk, and that using prepared SQL statements might also help performance. While I was rock solid about the first part, I also knew that there might be an impendance mismatch in how prepared statements work between Go's `sql` package and SQLite. I know this because that's what happened to the default Python SQLite driver, where preparing statements have no performance benefits. We'll just have to try that. While on that topic, there was also one more thing I wanted to try out: can we improve overall performance by running the SQLite code on a separate thread?

The intermediate version of `ingestData()` looked like this:

```go
func ingestData(db *sql.DB, rc io.ReadCloser) error {
        rd := bufio.NewReaderSize(rc, 4*1024*1024)

        c := make(chan HashData, 10000)
        go dbWriter(c, db)

        for {
                line, err := rd.ReadSlice(byte('\n'))
                if err != nil {
                        if err == io.EOF {
                                return nil
                        }
                        return err
                }
                c <- HashData{
                        Hash: string(line[0:40]),
                        Hash: string(line[41 : len(line)-2]),
                }
        }
}
```

This version creates a channel, spawns a goroutine to pick up data from this channel and insert into the database, and proceeds to undo the malloc microoptimisations I was trying to do: it allocates a new `HashData` struct on each iteration of the supposedly tight loop, and then allocates two pieces of string, one for the hash and another for the integer - usually just a few bytes long. It just looked so... inefficient?

I wrote the `dbWriter` to measure performance, and this will be our baseline version of the code:

```go
func dbWriter(c chan HashData, db *sql.DB) {
        var tx *sql.Tx
        var stmt *sql.Stmt
        count := 0

        startTime := time.Now()
        const recordThreshold = 100000

        beginTrans := func() (err error) {
                tx, err = db.Begin()
                if err != nil {
                        return
                }
                stmt, err = tx.Prepare("INSERT INTO hashes(hash, count) VALUES (?, ?)")
                return
        }
        err := beginTrans()
        if err != nil {
                fmt.Println(err)
                return
        }

        for data := range c {
                _, err = stmt.Exec(data.Hash, data.Count)
                if err != nil {
                        fmt.Println(err)
                        return
                }
                count++
                if count%recordThreshold == 0 {
                        // Do it in batches
                        err = tx.Commit()
                        if err != nil {
                                fmt.Println(err)
                                return
                        }
                        err = beginTrans()
                        if err != nil {
                                fmt.Println(err)
                                return
                        }
                        fmt.Print(".")
                        if *benchmarkMode && count >= 5*1000000 {
                                dur := time.Since(startTime)
                                fmt.Println("elapsed time:", dur, "that's", float64(count)/(dur.Seconds()), "recs/s")
                                os.Exit(0)
                        }
                }
        }
}
```

Running this on my laptop, I get about 340.000 records/s. Not bad, but not really stellar either.

What can be done about it? Should something be done at all?

Can we extract more parallelism from the SQL thread? Currently the process is using about 120% of a CPU core. 

I've instrumented the code and it's showing the channel is always full (`len(c) == 10000`) at the beginning of each batch of records to insert which kind of indicates decompression is much faster than SQLite???

Let's see what happens when I increase the channel size from 100,000 to 1,000,000 items:

```
-	c := make(chan HashData, 10000)
+	c := make(chan HashData, 100000)
```

Immediately, I'm observing the process CPU utilisation jumping to about 165% CPU - a decent upgrade for a single byte change, right? Let's see how full the channel is at each batch:

```
.46770.46770.84319.93540.45145.45145.91904.91904.43496.43496.90256.90256.41849.41849.88627.88627.40230.40230.
```

Seems like we're on a right track. The channel is never entirely full, and is often less than 50% full, so it SHOULD mean the decompressor and the SQLite code are parelellising nicely, right?

Wrong!

```
elapsed time: 14.855751469s that's 336569.9816958886 recs/s
```

Ouch. We did nothing for the performance, just managed to waste CPU cycles, memory and energy with the deep channel. Let's revert that "optimisation".

What next?

SQLite works with strings, and I'm not sure prepared statements will have an impact. Let's try using just `fmt.Sprintf()` instead of prepared statements (which should usually NEVER be done, but let's say we trust this data).

```
-               _, err = stmt.Exec(data.Hash, data.Count)
+               _, err = db.Exec(fmt.Sprintf("INSERT INTO hashes(hash, count) VALUES ('%s', %s)", data.Hash, data.Count))
```

How's the performance now?

```

```

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
