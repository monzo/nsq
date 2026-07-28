#!/bin/bash
# ./cert.sh foo@foo.com 127.0.0.1
# Found: https://gist.github.com/ncw/9253562#file-makecert-sh
#
# Regenerates the TLS fixtures used by the nsqd and nsqadmin test suites.
# nsqadmin/test holds byte-identical copies of ca/server/client/cert, so this
# script writes both locations to keep them in sync.

set -e

if [ "$1" == "" ]; then
    echo "Need email as argument"
    exit 1
fi

if [ "$2" == "" ]; then
    echo "Need CN as argument"
    exit 1
fi

PRIVKEY="test"
EMAIL=$1
CN=$2

rm -rf tmp
mkdir tmp
cd tmp

echo "make CA"
openssl req -new -x509 -days 3650 -newkey rsa:2048 -sha256 -keyout ca.key -out ca.pem \
    -config ../openssl.conf -extensions ca \
    -subj "/CN=ca" \
    -passout pass:$PRIVKEY

echo "make server cert"
openssl genrsa -out server.key 2048
openssl req -new -sha256 -key server.key -out server.req \
    -subj "/emailAddress=${EMAIL}/C=DE/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=${CN}"
openssl x509 -req -days 3650 -sha256 -in server.req -CA ca.pem -CAkey ca.key -CAcreateserial -passin pass:$PRIVKEY -out server.pem \
    -extfile ../openssl.conf -extensions server


echo "make client cert"
openssl genrsa -out client.key 2048
openssl req -new -sha256 -key client.key -out client.req \
    -subj "/emailAddress=${EMAIL}/C=DE/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=${CN}"
openssl x509 -req -days 3650 -sha256 -in client.req -CA ca.pem -CAkey ca.key -CAserial ca.srl -passin pass:$PRIVKEY -out client.pem \
    -extfile ../openssl.conf -extensions client

# Self-signed and intentionally unrelated to the CA above -- see the
# [standalone] section in openssl.conf for what the tests rely on.
echo "make standalone self-signed cert"
openssl req -new -x509 -days 3650 -newkey rsa:2048 -sha256 -nodes -keyout key.pem -out cert.pem \
    -config ../openssl.conf -extensions standalone \
    -subj "/C=US/ST=New York/L=New York City/O=NSQ/CN=test.local/emailAddress=${EMAIL}"

cd ..
mv tmp/* certs
rm -rf tmp

echo "syncing nsqadmin fixtures"
cp certs/* ../../nsqadmin/test/
