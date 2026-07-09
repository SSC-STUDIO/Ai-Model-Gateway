import sqlite3
import os
import pathlib

def check_db_with_wal(path, label):
    print(f"\n=== {label} ===")
    if not os.path.exists(path):
        print(f"  DB not found: {path}")
        return
    
    try:
        # Check file sizes
        db_path = path
        wal_path = path + "-wal"
        shm_path = path + "-shm"
        
        for f in [db_path, wal_path, shm_path]:
            if os.path.exists(f):
                size = os.path.getsize(f)
                print(f"  {os.path.basename(f)}: {size:,} bytes")
        
        # Try to read WAL file
        if os.path.exists(wal_path):
            wal_size = os.path.getsize(wal_path)
            if wal_size > 32:  # WAL header is 32 bytes
                print(f"  WAL has uncommitted data ({wal_size:,} bytes)")
            else:
                print(f"  WAL clean (header only)")
        
        # Connect and check
        conn = sqlite3.connect(path)
        cursor = conn.cursor()
        
        # Check all tables
        cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
        tables = cursor.fetchall()
        
        total_rows = 0
        for table in tables:
            name = table[0]
            try:
                cursor.execute(f"SELECT COUNT(*) FROM [{name}]")
                count = cursor.fetchone()[0]
                print(f"  {name}: {count:,} rows")
                total_rows += count
            except:
                pass
        
        print(f"  Total: {total_rows:,} rows across {len(tables)} tables")
        conn.close()
    except Exception as e:
        print(f"  Error: {e}")

# Derive paths relative to script location
script_dir = pathlib.Path(__file__).resolve().parent
deploy_root = script_dir

# Also check the source build directory if it exists
src_root = script_dir.parent / "Ai-Model-Gateway-src"

print(f"Deploy root: {deploy_root}")

# Check current deployment directory DBs
deploy_events = deploy_root / ".gateway-runtime" / "telemetry-migrated" / "events.db"
deploy_query = deploy_root / ".gateway-runtime" / "telemetry-migrated" / "query.db"

check_db_with_wal(str(deploy_events), f"Events DB ({deploy_root.name})")
check_db_with_wal(str(deploy_query), f"Query DB ({deploy_root.name})")

# Check source build directory DBs if it exists
if src_root.exists():
    print(f"\nSource root: {src_root}")
    src_events = src_root / ".gateway-runtime" / "telemetry-migrated" / "events.db"
    src_query = src_root / ".gateway-runtime" / "telemetry-migrated" / "query.db"
    
    check_db_with_wal(str(src_events), f"Events DB ({src_root.name})")
    check_db_with_wal(str(src_query), f"Query DB ({src_root.name})")
else:
    print(f"\nSource root not found at {src_root}, skipping.")
