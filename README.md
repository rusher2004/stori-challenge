# stori-challenge

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
aws cloudformation delete-stack --stack-name stori-test
```