import sqlite3
import pathlib

def check_db(path, label):
    print(f"\n=== {label} ===")
    try:
        conn = sqlite3.connect(path)
        cursor = conn.cursor()
        cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
        tables = cursor.fetchall()
        print(f"Tables: {[t[0] for t in tables]}")
        for table in tables:
            name = table[0]
            try:
                cursor.execute(f"SELECT COUNT(*) FROM [{name}]")
                count = cursor.fetchone()[0]
                print(f"  {name}: {count} rows")
                if count > 0 and count < 100:
                    cursor.execute(f"SELECT * FROM [{name}] LIMIT 5")
                    rows = cursor.fetchall()
                    for row in rows:
                        print(f"    {row}")
            except:
                pass
        conn.close()
    except Exception as e:
        print(f"Error: {e}")

# Derive paths relative to script location
script_dir = pathlib.Path(__file__).resolve().parent
old_db = script_dir.parent / "Ai-Model-Gateway-src" / ".gateway-runtime" / "telemetry-migrated" / "query.db"
new_db = script_dir / ".gateway-runtime" / "telemetry-migrated" / "query.db"

check_db(str(old_db), "Old Query DB (src)")
check_db(str(new_db), "New Query DB (deploy)")
