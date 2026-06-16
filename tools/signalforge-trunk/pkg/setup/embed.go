package setup

import _ "embed"

//go:embed samples/okwin-bundle.csv
var okwinBundleCSV []byte

const bundledSampleName = "okwin-bundle.csv"
