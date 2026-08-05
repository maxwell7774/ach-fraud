#!/usr/bin/env python3
"""Multi-entry, multi-batch PPD credit generator for seeding.
Usage: seed.py OUT --date YYMMDD [--rdfi RDFI] --batch "acct,amount,name;acct,amount,name" [--batch ...]
"""
import sys

def pad(s, n):
    return (s + " " * n)[:n]

def znum(n, width):
    return str(n).rjust(width, "0")

def main():
    argv = sys.argv[1:]
    out, date, rdfi = argv[0], None, "231380104"
    batches = []
    i = 1
    while i < len(argv):
        if argv[i] == "--date":
            date = argv[i + 1]
            i += 2
        elif argv[i] == "--rdfi":
            rdfi = argv[i + 1]
            i += 2
        elif argv[i] == "--batch":
            entries = []
            for part in argv[i + 1].split(";"):
                acct, amt, name = part.split(",")
                entries.append((acct, int(amt), name))
            batches.append(entries)
            i += 2
        else:
            raise SystemExit("bad arg: " + argv[i])

    rdfi8, check = rdfi[:8], rdfi[8:]
    lines = []
    lines.append("101" + pad("231380104", 10) + pad("121042882", 10) + date + "0000A" + "09410" + "1"
                 + pad("Federal Reserve Bank", 23) + pad("My Bank Name", 23) + pad("", 8))
    total_hash = 0
    total_amt = 0
    total_entries = 0
    for bi, entries in enumerate(batches, start=1):
        lines.append("5" + "220" + pad("Name on Account", 16) + pad("", 20) + pad("121042882", 10)
                     + "PPD" + pad("REG.SALARY", 10) + pad("", 6) + date + "   " + "1" + "12104288" + znum(bi, 7))
        batch_hash = 0
        batch_amt = 0
        for ei, (acct, amt, name) in enumerate(entries, start=1):
            lines.append("6" + "22" + rdfi8 + check + pad(acct, 17) + znum(amt, 10)
                         + pad("", 15) + pad(name, 22) + "  " + "0" + "12104288" + znum(ei, 7))
            batch_hash += int(rdfi8)
            batch_amt += amt
        lines.append("8" + "220" + znum(len(entries), 6) + znum(batch_hash, 10) + znum(0, 12) + znum(batch_amt, 12)
                     + pad("121042882", 10) + "12104288" + znum(bi, 7) + pad("", 27))
        total_hash += batch_hash
        total_amt += batch_amt
        total_entries += len(entries)
    lines.append("9" + znum(len(batches), 6) + znum(len(batches), 6) + znum(total_hash, 10)
                 + znum(0, 12) + znum(total_amt, 12) + pad("", 39))
    open(out, "w").write("\n".join(l.ljust(94) for l in lines) + "\n")

main()
