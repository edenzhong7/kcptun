#!/bin/bash

CONF_FILE=`pwd`/openssl.conf

# Generate CA key and certificate
openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -out ca.crt -config $CONF_FILE

# Generate server key and certificate
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -config $CONF_FILE
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -sha256 -extfile $CONF_FILE -extensions req_ext

# Generate client key and certificate
openssl genrsa -out client.key 2048
openssl req -new -key client.key -out client.csr -config $CONF_FILE
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -days 365 -sha256 -extfile $CONF_FILE -extensions req_ext

# Clean up intermediate files
rm -f *.csr
#rm -f *.srl
