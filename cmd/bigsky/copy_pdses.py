#!/usr/bin/env python3
#
# pip install requests
#
# python3 copy_pdses.py --admin-key hunter2 --source-url http://srcrelay:2470 --dest-url http://destrelay:2470

import json
import logging
import sys
import urllib.parse

import requests

logger = logging.getLogger(__name__)

class relay:
    def __init__(self, rooturl, headers=None, session=None):
        "rooturl string, headers dict or None, session requests.Session() or None"
        self.rooturl = rooturl
        self.headers = headers or dict()
        self.session = session or requests.Session()

    def crawl(self, host):
        pheaders = dict(self.headers)
        pheaders['Content-Type'] = 'application/json'
        url = urllib.parse.urljoin(self.rooturl, '/admin/pds/requestCrawl')
        response = self.session.post(url, headers=pheaders, data=json.dumps({"hostname": host}))
        if response.status_code != 200:
            return False
        return True

    def crawlAndSetLimits(self, host, limits):
        "host string, limits dict"
        if not self.crawl(host):
            logger.error("%s %s : %d %r", url, host, response.status_code, response.text)
            return
        if limits is None:
            logger.debug("requestCrawl %s OK", host)
        if self.setLimits(host, limits):
            logger.debug("requestCrawl + changeLimits %s OK", host)
    def setLimits(self, host, limits):
        url = urllib.parse.urljoin(self.rooturl, '/admin/pds/changeLimits')
        plimits = dict(limits)
        plimits["host"] = host
        pheaders = dict(self.headers)
        pheaders['Content-Type'] = 'application/json'
        response = self.session.post(url, headers=pheaders, data=json.dumps(plimits))
        if response.status_code != 200:
            logger.error("%s %s : %d %r", url, host, response.status_code, response.text)
            return False
        return True

    def crawlAndBlock(self, host):
        "make relay aware of PDS, and block it"
        if not self.crawl(host):
            logger.error("%s %s : %d %r", url, host, response.status_code, response.text)
            return
        if self.block(host):
            logger.debug("requestCrawl + block %s OK", host)

    def block(self, host):
        url = urllib.parse.urljoin(self.rooturl, '/admin/pds/block')
        response = self.session.post(url, headers=self.headers, data='', params={"host":host})
        if response.status_code != 200:
            logger.error("%s %s : %d %r", url, host, response.status_code, response.text)
            return False
        return True

    def unblock(self, host):
        url = urllib.parse.urljoin(self.rooturl, '/admin/pds/unblock')
        response = self.session.post(url, headers=self.headers, data='', params={"host":host})
        if response.status_code != 200:
            logger.error("%s %s : %d %r", url, host, response.status_code, response.text)
            return False
        return True

    def pdsList(self):
        "GET /admin/pds/list"
        url = urllib.parse.urljoin(self.rooturl, '/admin/pds/list')
        response = self.session.get(url, headers=self.headers)
        if response.status_code != 200:
            logger.error("%s : %d %r", url, response.status_code, response.text)
            return None
        return response.json()

def makeByHost(they):
    out = dict()
    for rec in they:
        out[rec['Host']] = rec
    return out

def makeLimits(rec):
    "for submitting to changeLimits"
    return {
        "host": rec['Host'],
        "per_second":rec['RateLimit'],
        "per_hour":rec['HourlyEventLimit'],
        "per_day":rec['DailyEventLimit'],
        "crawl_rate":rec['CrawlRateLimit'],
        "repo_limit":rec['RepoLimit'],
    }

def makeRequestCrawl(rec):
    "for submitting to requestCrawl"
    return {"hostname":rec["Host"]}

def de(a,b):
    # dict equal
    for ka, va in a.items():
        vb = b[ka]
        if (va is None) and (vb is None):
            continue
        if va == vb:
            continue
        return False
    for kb in b.keys():
        if kb not in a:
            return False
    return True

def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument('--admin-key', default=None, help='relay auth bearer token', required=True)
    ap.add_argument('--source-url', default=None, help='base url to GET /admin/pds/list')
    ap.add_argument('--source-json', default=None, help='load /admin/pds/list json from file')
    ap.add_argument('--dest-url', default=None, help='dest URL to POST requestCrawl etc to')
    ap.add_argument('--dry-run', default=False, action='store_true')
    ap.add_argument('--verbose', default=False, action='store_true')
    args = ap.parse_args()

    if args.verbose:
        logging.basicConfig(level=logging.DEBUG)
    else:
        logging.basicConfig(level=logging.INFO)

    headers = {'Authorization': 'Bearer ' + args.admin_key}

    if args.source_json:
        with open(args.source_json, 'rt') as fin:
            sourceList = json.load(fin)
    elif args.source_url:
        relaySession = relay(args.source_url, headers)
        sourceList = relaySession.pdsList()
    else:
        sys.stdout.write("need --source-url or --source-json\n")
        sys.exit(1)

    r2 = relay(args.dest_url, headers)
    destList = r2.pdsList()

    source = makeByHost(sourceList)
    dests = makeByHost(destList)

    snotd = []
    dnots = []
    diflim = []
    difblock = []
    recrawl = []

    for k1, v1 in source.items():
        v2 = dests.get(k1)
        if v2 is None:
            snotd.append(v1)
            continue
        lim1 = makeLimits(v1)
        lim2 = makeLimits(v2)
        if v1["Blocked"] != v2["Blocked"]:
            difblock.append((k1,v1["Blocked"]))
        if v1["Blocked"]:
            continue
        if not de(lim1, lim2):
            diflim.append(lim1)
        if v1["HasActiveConnection"] and not v2["HasActiveConnection"]:
            recrawl.append(k1)
    for k2 in dests.keys():
        if k2 not in source:
            dnots.append(k2)

    logger.debug("%d source not dest", len(snotd))
    for rec in snotd:
        if rec["Blocked"]:
            if args.dry_run:
                sys.stdout.write("crawl and block: {!r}\n".format(rec["Host"]))
            else:
                r2.crawlAndBlock(rec["Host"])
        else:
            limits = makeLimits(rec)
            if args.dry_run:
                sys.stdout.write("crawl and limit: {}\n".format(json.dumps(limits)))
            else:
                r2.crawlAndSetLimits(rec["Host"], limits)
    logger.debug("adjust limits: %d", len(diflim))
    for limits in diflim:
        if args.dry_run:
            sys.stdout.write("set limits: {}\n".format(json.dumps(limits)))
        else:
            r2.setLimits(limits["host"], limits)
    logger.debug("adjust block status: %d", len(difblock))
    for host, blocked in difblock:
        if args.dry_run:
            sys.stdout.write("{} block={}\n".format(host, blocked))
        else:
            if blocked:
                r2.block(host)
            else:
                r2.unblock(host)
    logger.debug("restart requestCrawl: %d", len(recrawl))
    for host in recrawl:
        if args.dry_run:
            logger.info("requestCrawl %s", host)
        else:
            if r2.crawl(host):
                logger.debug("requestCrawl %s OK", host)
    logger.info("%d in dest but not source", len(dnots))
    for k2 in dnots:
        logger.debug("%s", k2)





if __name__ == '__main__':
    main()
