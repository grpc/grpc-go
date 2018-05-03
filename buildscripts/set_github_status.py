#!/usr/bin/env python2.7
#
# Copyright 2018 The gRPC Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import argparse
import json
import urllib2


def run():
  argp = argparse.ArgumentParser(description='Set status on pull request')

  argp.add_argument(
      '--sha1', type=str, help='SHA1 of the commit', required=True)
  argp.add_argument(
      '--state',
      type=str,
      choices=('error', 'failure', 'pending', 'success'),
      help='State to set',
      required=True)
  argp.add_argument(
      '--description', type=str, help='Status description', required=True)
  argp.add_argument('--context', type=str, help='Status context', required=True)
  argp.add_argument(
      '--oauth_file', type=str, help='File with OAuth token', required=True)

  args = argp.parse_args()
  sha1 = args.sha1
  state = args.state
  description = args.description
  context = args.context
  oauth_file = args.oauth_file

  with open(oauth_file, 'r') as oauth_file_reader:
    oauth_token = oauth_file_reader.read().replace('\n', '')

  req = urllib2.Request(
      url='https://api.github.com/repos/grpc/grpc-java/statuses/%s' % sha1,
      data=json.dumps({
          'state': state,
          'description': description,
          'context': context,
      }),
      headers={
          'Authorization': 'token %s' % oauth_token,
          'Content-Type': 'application/json',
      })
  print urllib2.urlopen(req).read()


if __name__ == '__main__':
  run()
