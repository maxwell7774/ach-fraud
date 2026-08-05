import os
import datetime
def pad(s, n): return (s + " " * n)[:n]
def znum(n, width): return str(n).rjust(width, "0")
today = datetime.date.today(); eff = today.strftime("%y%m%d")
lines = []
lines.append("101" + pad("231380104",10) + pad("121042882",10) + eff + "0000A" + "09410" + "1"
             + pad("Federal Reserve Bank",23) + pad("My Bank Name",23) + pad("",8))
def batch(company, acct, amt, name):
    lines.append("5" + "220" + pad(company,16) + pad("",20) + pad("121042882",10)
                 + "PPD" + pad("REG.SALARY",10) + pad("",6) + eff + "   " + "1" + "12104288" + znum(1,7))
    lines.append("6" + "22" + "23138010" + "4" + pad(acct,17) + znum(amt,10)
                 + pad("",15) + pad(name,22) + "  " + "0" + "12104288" + znum(1,7))
    lines.append("8" + "220" + znum(1,6) + znum(23138010,10) + znum(0,12) + znum(amt,12)
                 + pad("121042882",10) + "12104288" + znum(1,7) + pad("",27))
batch("Batch One Co", "123450011", 60000, "Receiver K")
batch("Batch Two Co", "123450012", 150000, "Receiver L")
total = 60000+150000
lines.append("9" + znum(2,6) + znum(1,6) + znum(2,6) + znum(23138010*2,10) + znum(0,12) + znum(total,12) + pad("",39))
open(os.path.join(os.environ.get("ACH_GEN_OUT", "."), "multi.ach"), "w").write("\n".join(l.ljust(94) for l in lines) + "\n")
