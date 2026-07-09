import sqlite3

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
                cursor.execute(f"SELECT COUNT(*) FROM {name}")
                count = cursor.fetchone()[0]
                print(f"  {name}: {count} rows")
                if count > 0 and count < 100:
                    cursor.execute(f"SELECT * FROM {name} LIMIT 5")
                    rows = cursor.fetchall()
                    for row in rows:
                        print(f"    {row}")
            except:
                pass
        conn.close()
    except Exception as e:
        print(f"Error: {e}")

check_db(r"D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway-src\.gateway-runtime\telemetry-migrated\query.db", "Old Query DB")
check_db(r"D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway\.gateway-runtime\telemetry-migrated\query.db", "New Query DB")
