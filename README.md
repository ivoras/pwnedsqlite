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

But you know what would be even more awsome? Doing it with a proper file format.

Sure, I could make my own B-tree engine, but why bother? There is an universal database format out there which everyone sane can use nowadays: SQLite. The sure-fire way to make the original performance goal must be to create an indexed SQLite table.

## Decompressing on the fly

For some reason, I thought it a bit dirty to decompress the orginal file first, so I found a 7zip decompression library (`github.com/bodgit/sevenzip`) and used that to read data directly from the compressed archive. It has a really nice interface where the code to read the txt file boiled down to:

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
