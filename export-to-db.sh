#!/bin/bash


python3 create_db.py && python3 validate.py
# и, если нужно заполнить оставшиеся дыры словарём (сейчас безопасно):
python3 apply_db_translations.py --all --dry-run   # сначала dry!
