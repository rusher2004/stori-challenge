package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
)

var months map[int]string

type EmailAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
}

type Summaries struct {
	CreditCount         int
	CreditTotal         float64
	DebitCount          int
	DebitTotal          float64
	MonthlyTransactions map[string]int
}

type TransactionCSV struct {
	ID          string
	Date        string
	Transaction string
}

func HandleRequest(ctx context.Context, ev events.S3Event) (string, error) {
	out := ""
	for _, record := range ev.Records {
		s3 := record.S3
		fmt.Printf("[%s - %s] Bucket = %s, Key = %s \n", record.EventSource, record.EventTime, s3.Bucket.Name, s3.Object.Key)
		out += fmt.Sprintf("[%s - %s] Bucket = %s, Key = %s \n", record.EventSource, record.EventTime, s3.Bucket.Name, s3.Object.Key)
	}

	file, err := getFile(ev)
	if err != nil {
		return "ERROR", err
	}

	ts, err := readCSV(file)
	if err != nil {
		return "ERROR", err
	}
	sums, err := getSummaries(ts)
	if err != nil {
		return "ERROR", err
	}
	fmt.Printf("sums: %v\n", sums)

	if err = sendEmail(sums); err != nil {
		return "ERROR", err
	}

	return out, nil
}

func getFile(ev events.S3Event) (*os.File, error) {
	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return nil, err
	}

	// we're making some assumptions here, but for this code challenge purpose we should be fine.
	// the s3 trigger filter ensures we're getting a file with path `csv/somefile.csv`
	name := strings.Split(ev.Records[0].S3.Object.Key, "/")[1]
	file, err := os.Create(filepath.Join("/tmp", name))
	// We should be creating a unique name of some kind instead of just using what's in the key
	// because os.Create will truncate if the file at that path already exists
	if err != nil {
		return nil, err
	}

	downloader := s3manager.NewDownloader(sess)

	bucket := ev.Records[0].S3.Bucket.Name
	key := ev.Records[0].S3.Object.URLDecodedKey
	_, err = downloader.Download(file, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	return file, nil
}

func getMonth(s string) (string, error) {
	split := strings.Split(s, "/")
	intStr := split[0]
	i, err := strconv.Atoi(intStr)
	if err != nil {
		return "", err
	}

	return months[i], nil
}

func getSummaries(ts []TransactionCSV) (Summaries, error) {
	sm := Summaries{}
	monthTotals := make(map[string]int)
	for _, t := range ts {
		month, err := getMonth(t.Date)
		if err != nil {
			return Summaries{}, err
		}
		monthTotals[month]++

		amt, err := strconv.ParseFloat(t.Transaction, 64)
		fl := amt
		if err != nil {
			return Summaries{}, err
		}
		if amt > 0 {
			sm.CreditCount++
			sm.CreditTotal += fl
		}
		if amt < 0 {
			sm.DebitCount++
			sm.DebitTotal += fl
		}
	}
	sm.MonthlyTransactions = monthTotals

	return sm, nil
}

func readCSV(f *os.File) ([]TransactionCSV, error) {
	r := csv.NewReader(f)

	if _, err := r.Read(); err != nil {
		return []TransactionCSV{}, err
	}

	rows, err := r.ReadAll()
	if err != nil {
		return []TransactionCSV{}, nil
	}

	var ts []TransactionCSV
	for _, r := range rows {
		ts = append(ts, TransactionCSV{ID: r[0], Date: r[1], Transaction: r[2]})
	}

	return ts, nil
}

func sendEmail(s Summaries) error {
	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return err
	}
	ss := secretsmanager.New(sess)

	input := secretsmanager.GetSecretValueInput{
		SecretId: aws.String("EMAIL_SECRET"),
	}
	sv, err := ss.GetSecretValue(&input)
	if err != nil {
		return err
	}

	var ea EmailAuth
	if err = json.Unmarshal([]byte(*sv.SecretString), &ea); err != nil {
		return err
	}

	auth := smtp.PlainAuth("", ea.Username, ea.Password, ea.Host)

	tpl := `
	<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN"
	"http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
	<html>

	</head>

	<body>
		<p>Hello Customer,</p>
		<p>Here is a summary of your latest transactions:</p>

		<p>Total Balance: {{.Total}}</p>
		{{range $month, $count := .MonthlyTransactions}}<p>{{ $month }}: {{ $count }}</p>{{end}}
		<p>Average debit amount: {{ .DebitAverage }}</p>
		<p>Average credit amount: {{ .CreditAverage }}</p>
	</body>

	</html>
`
	to := math.Round((s.CreditTotal+s.DebitTotal)*100) / 100
	ca := math.Round(s.CreditTotal/float64(s.CreditCount)*100) / 100
	da := math.Round(s.DebitTotal/float64(s.DebitCount)*100) / 100

	data := struct {
		Total               float64
		MonthlyTransactions map[string]int
		CreditAverage       float64
		DebitAverage        float64
	}{
		Total:               to,
		MonthlyTransactions: s.MonthlyTransactions,
		CreditAverage:       ca,
		DebitAverage:        da,
	}

	t, err := template.New("email").Parse(tpl)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	if err := t.Execute(buf, data); err != nil {
		return err
	}
	body := buf.String()

	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	subj := "Subject: Transaction Summary\n"
	msg := []byte(subj + mime + "\n" + body)

	if err := smtp.SendMail(ea.Host+":587", auth, ea.Username, []string{ea.Username}, msg); err != nil {
		return err
	}

	return nil
}

func main() {
	months = map[int]string{
		1:  "January",
		2:  "February",
		3:  "March",
		4:  "April",
		5:  "May",
		6:  "June",
		7:  "July",
		8:  "August",
		9:  "September",
		10: "October",
		11: "November",
		12: "December",
	}

	lambda.Start(HandleRequest)
}
