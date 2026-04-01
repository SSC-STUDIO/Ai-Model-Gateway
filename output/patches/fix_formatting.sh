#!/bin/bash
# 修复 Go 代码格式化问题
# 使用方法: ./fix_formatting.sh

echo "Running gofmt to fix formatting issues..."
gofmt -w .

echo "Done. Files fixed:"
gofmt -l .
