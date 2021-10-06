# stori-challenge

This is an AWS Lambda function written in Go, deployed with AWS CloudFormation. It is triggered when a file is uploaded to its corresponding S3 bucket in the `csv/` directory. AWS Secrets Manager is used to store and access email credentials.

Some assumptions are made about the data being properly formatted, so there isn't any input validation happening. It is expecting a cs in the following format, with no empty fields.

| Id  | Date | Transaction |
| --- | ---- | ----------- |
| 0   | 7/15 | +60.5       |
| 1   | 7/28 | -10.3       |
| 2   | 8/2  | -20.46      |
| 3   | 8/13 | +10         |

The Lambda handler will process the file and send an email with a summary of its contents. For this test, the email will be sent to the same address you use in the `EMAIL_SECRET` below.

## Requirements

 - [AWS CLI](https://aws.amazon.com/cli/)
 - [Go](https://go.dev/)

## Setup
 
create bucket to upload Go binary for lambda function
```sh
aws s3 mb s3://storibucket
```

store email credentials in Secrets Manager for sending emails
```sh
create-secret --name EMAIL_SECRET --secret-string `{"username":"$USERNAME","password":"$PASSWORD","host":"$HOST"}'
```
note: $HOST will need to include the subdomain, like `smtp.gmail.com`

## Deploy

```sh
cd lambda

GOOS=linux go build main.go

cd ..

aws cloudformation package --template-file template.yaml --s3-bucket storibucket --output-template-file out.yaml

aws cloudformation deploy --template-file out.yaml --stack-name stori-test --capabilities CAPABILITY_NAMED_IAM
```

## Invoke

```sh
BUCKET=$(aws cloudformation describe-stack-resource --stack-name stori-test --logical-resource-id bucket --query 'StackResourceDetail.PhysicalResourceId' --output text)

aws s3 cp sample.csv s3://$BUCKET/csv/
```

## Teardown

```sh
# empty and delete deployment bucket
aws s3 rb s3://storibucket --force

# empty csv bucket before deleting stack
BUCKET=$(aws cloudformation describe-stack-resource --stack-name stori-test --logical-resource-id bucket --query 'StackResourceDetail.PhysicalResourceId' --output text)

aws s3 rm s3://$BUCKET --recursive

aws cloudformation delete-stack --stack-name stori-test
```