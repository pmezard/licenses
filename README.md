[![Build Status](https://travis-ci.org/pmezard/licenses.png?branch=master)](https://travis-ci.org/pmezard/licenses)

# What is it?

`licenses` uses `go list` tool over a Go workspace to collect the dependencies
of a package or command, detect their license if any and match them against
well-known templates.

```
$ licenses github.com/blevesearch/bleve
github.com/blevesearch/bleve             Apache License 2.0
github.com/blevesearch/go-porterstemmer  MIT License (93%)
github.com/blevesearch/segment           Apache License 2.0
github.com/boltdb/bolt                   MIT License (97%)
github.com/golang/protobuf/proto         BSD 3-clause "New" or "Revised" License (91%)
github.com/steveyen/gtreap               MIT License (96%)
vendor/golang.org/x/net/http2/hpack      ?
```

Unmatched license words can be displayed with:
```
$ licenses -w github.com/steveyen/gtreap
github.com/steveyen/gtreap  MIT License (98%)
                            -words: mit, license
```

# Where does it come from?

Both the code and reference data were directly ported from:

  [https://github.com/benbalter/licensee](https://github.com/benbalter/licensee)
