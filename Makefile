#! /usr/bin/make
#
# Makefile for Golang projects, v0.9.0
#
# Features:
# - runs ginkgo tests recursively, computes code coverage report
# - code coverage ready for travis-ci to upload and produce badges for README.md
# - to include the build status and code coverage badge in CI use (replace NAME by what
#   you set $(NAME) to further down, and also replace magnum.travis-ci.com by travis-ci.org for
#   publicly accessible repos [sigh]):
#   [![Build Status](https://magnum.travis-ci.com/rightscale/NAME.svg?token=4Q13wQTY4zqXgU7Edw3B&branch=master)](https://magnum.travis-ci.com/rightscale/NAME
#   ![Code Coverage](https://s3.amazonaws.com/rs-code-coverage/NAME/cc_badge_master.svg)
#
# Top-level targets:
# default: same as test
# test: runs unit tests recursively and produces code coverage stats and shows them
# travis-test: just runs unit tests recursively
# clean: removes build stuff
#
# HACKS - a couple of things here are unconventional in order to keep travis-ci fast:
# - use 'godep save' on your laptop if you add dependencies, but we don't use godep in the
#   makefile, instead, we simply add the godep workspace to the GOPATH

NAME=gojiutil
# dependencies that are not in Godep because they're used by the build&test process
DEPEND=golang.org/x/tools/cmd/cover github.com/onsi/ginkgo/ginkgo \
       github.com/rlmcpherson/s3gof3r/gof3r github.com/tools/godep

#=== below this line ideally remains unchanged, add new targets at the end  ===

TRAVIS_BRANCH?=dev
DATE=$(shell date '+%F %T')
TRAVIS_COMMIT?=$(shell git symbolic-ref HEAD | cut -d"/" -f 3)
# by manually adding the godep workspace to the path we don't need to run godep itself
GOPATH:=$(PWD)/Godeps/_workspace:$(GOPATH)
# because of the Godep path we build ginkgo into the godep workspace
PATH:=$(PWD)/Godeps/_workspace/bin:$(PATH)

# the default target runs tests
default: test

# Installing build dependencies is a bit of a mess. Don't want to spend lots of time in
# Travis doing this. The folllowing just relies on go get no reinstalling when it's already
# there, like your laptop.
depend:
	go get $(DEPEND)
	godep restore

clean:
	rm -rf *.coverprofile

# gofmt uses the awkward *.go */*.go because gofmt -l . descends into the Godeps workspace
# and then pointlessly complains about bad formatting in imported packages, sigh
lint:
	@if gofmt -l *.go | grep .go; then \
	  echo "^- Repo contains improperly formatted go files; run gofmt -w *.go" && exit 1; \
	  else echo "All .go files formatted correctly"; fi
	go tool vet -composites=false *.go
	#go tool vet -composites=false **/*.go

travis-test: cover

# running ginkgo twice, sadly, the problem is that -cover modifies the source code with the effect
# that if there are errors the output of gingko refers to incorrect line numbers
# tip: if you don't like colors use gingkgo -r -noColor
test: lint
	ginkgo -r
	ginkgo -r -cover
	go tool cover -func=`basename $$PWD`.coverprofile

cover: lint
	ginkgo -r --randomizeAllSpecs --randomizeSuites --failOnPending -cover
	@echo 'mode: atomic' >_total
	@for f in `find . -name \*.coverprofile`; do tail -n +2 $$f >>_total; done
	@mv _total total.coverprofile
	@COVERAGE=$$(go tool cover -func=total.coverprofile | grep "^total:" coverage.txt | grep -o "[0-9\.]*");\
	  echo "*** Code Coverage is $$COVERAGE% ***"
