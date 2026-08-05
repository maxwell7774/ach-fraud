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
lines.append("6" + "22" + "23138010" + "4" + pad("998877001",17) + znum(2500,10)
             + pad("",15) + pad("Small",22) + "  " + "0" + "12104288" + znum(1,7))
lines.append("8" + "220" + znum(1,6) + znum(23138010,10) + znum(0,12) + znum(2500,12)
             + pad("121042882",10) + "12104288" + znum(1,7) + pad("",27))
lines.append("9" + znum(1,6) + znum(1,6) + znum(23138010,10) + znum(0,12) + znum(2500,12) + pad("",39))
open(os.path.join(os.environ.get("ACH_GEN_OUT", "."), "small.ach"), "w").write("\n".join(l.ljust(94) for l in lines) + "\n")
