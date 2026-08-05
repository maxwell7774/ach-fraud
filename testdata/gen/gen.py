import os
import datetime
def pad(s, n): return (s + " " * n)[:n]
def znum(n, width): return str(n).rjust(width, "0")
today = datetime.date.today(); eff = today.strftime("%y%m%d")
lines = []
lines.append("101" + pad("231380104",10) + pad("121042882",10) + eff + "0000A" + "09410" + "1"
             + pad("Federal Reserve Bank",23) + pad("My Bank Name",23) + pad("",8))
lines.append("5" + "220" + pad("Name on Account",16) + pad("",20) + pad("121042882",10)
             + "PPD" + pad("REG.SALARY",10) + pad("",6) + eff + "   " + "1" + "12104288" + znum(1,7))
entries = [("123450001",60000,"Receiver A"),("123450001",60000,"Receiver A"),
           ("123450002",150000,"Receiver B"),("123450009",5000,"Receiver Z")]
for i,(acct,amt,name) in enumerate(entries, start=1):
    lines.append("6" + "22" + "23138010" + "4" + pad(acct,17) + znum(amt,10)
                 + pad("",15) + pad(name,22) + "  " + "0" + "12104288" + znum(i,7))
lines.append("8" + "220" + znum(4,6) + znum(23138010*4,10) + znum(0,12) + znum(60000+60000+150000+5000,12)
             + pad("121042882",10) + "12104288" + znum(1,7) + pad("",27))
lines.append("9" + znum(1,6) + znum(1,6) + znum(23138010*4,10) + znum(0,12) + znum(60000+60000+150000+5000,12) + pad("",39))
open(os.path.join(os.environ.get("ACH_GEN_OUT", "."), "credits.ach"), "w").write("\n".join(l.ljust(94) for l in lines) + "\n")
