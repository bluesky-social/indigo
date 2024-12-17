#!/usr/bin/env python3
#
# pip install requests
#
# python3 resync_pdses.py --admin-key hunter2 --url http://myrelay:2470 host_per_line.txt

import json
import sys
import urllib.parse

import requests


# pds limits for POST /admin/pds/changeLimits
# {"host":"", "per_second": int, "per_hour": int, "per_day": int, "crawl_rate": int, "repo_limit": int}

limitsKeys = ('per_second', 'per_hour', 'per_day', 'crawl_rate', 'repo_limit')

def checkLimits(limits):
    for k in limits.keys():
        if k not in limitsKeys:
            raise Exception(f"unknown pds rate limits key {k!r}")
    return True

class relay:
    def __init__(self, rooturl, headers=None, session=None):
        "rooturl string, headers dict or None, session requests.Session() or None"
        self.rooturl = rooturl
        self.headers = headers or dict()
        self.session = session or requests.Session()

    def resync(self, host):
        "host string"
        url = urllib.parse.urljoin(self.rooturl, '/admin/pds/resync')
        response = self.session.post(url, params={"host": host}, headers=self.headers, data='')
        if response.status_code != 200:
            sys.stderr.write(f"{url}?host={host} : ({response.status_code}) ({response.text!r})\n")
        else:
            sys.stderr.write(f"{url}?host={host} : OK\n")

    def crawlAndSetLimits(self, host, limits):
        "host string, limits dict"
        pheaders = dict(self.headers)
        pheaders['Content-Type'] = 'application/json'
        url = urllib.parse.urljoin(self.rooturl, '/admin/pds/requestCrawl')
        response = self.session.post(url, headers=pheaders, data=json.dumps({"hostname": host}))
        if response.status_code != 200:
            sys.stderr.write(f"{url} {host} : {response.status_code} {response.text!r}\n")
            return
        if limits is None:
            sys.stderr.write(f"requestCrawl {host} OK\n")
        url = urllib.parse.urljoin(self.rooturl, '/admin/pds/changeLimits')
        plimits = dict(limits)
        plimits["host"] = host
        response = self.session.post(url, headers=pheaders, data=json.dumps(plimits))
        if response.status_code != 200:
            sys.stderr.write(f"{url} {host} : {response.status_code} {response.text!r}\n")
            return
        sys.stderr.write(f"requestCrawl + changeLimits {host} OK\n")

def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument('input', default='-', help='host per line text file to read, - for stdin')
    ap.add_argument('--admin-key', default=None, help='relay auth bearer token', required=True)
    ap.add_argument('--url', default=None, help='base url to POST /admin/pds/resync', required=True)
    ap.add_argument('--resync', default=False, action='store_true', help='resync selected PDSes')
    ap.add_argument('--limits', default=None, help='json pds rate limits')
    ap.add_argument('--crawl', default=False, action='store_true', help='crawl & set limits')
    args = ap.parse_args()

    headers = {'Authorization': 'Bearer ' + args.admin_key}

    relaySession = relay(args.url, headers)

    #url = urllib.parse.urljoin(args.url, '/admin/pds/resync')

    #sess = requests.Session()
    if args.crawl and args.resync:
        sys.stderr.write("should only specify one of --resync --crawl")
        sys.exit(1)
    if (not args.crawl) and (not args.resync):
        sys.stderr.write("should specify one of --resync --crawl")
        sys.exit(1)

    limits = None
    if args.limits:
        limits = json.loads(args.limits)
        checkLimits(limits)

    if args.input == '-':
        fin = sys.stdin
    else:
        fin = open(args.input, 'rt')
    for line in fin:
        if not line:
            continue
        line = line.strip()
        if not line:
            continue
        if line[0] == '#':
            continue
        host = line
        if args.crawl:
            relaySession.crawlAndSetLimits(host, limits)
        elif args.resync:
            relaySession.resync(host)
        # response = sess.post(url, params={"host": line}, headers=headers)
        # if response.status_code != 200:
        #     sys.stderr.write(f"{url}?host={line} : ({response.status_code}) ({response.text!r})\n")
        # else:
        #     sys.stderr.write(f"{url}?host={line} : OK\n")

if __name__ == '__main__':
    main()
