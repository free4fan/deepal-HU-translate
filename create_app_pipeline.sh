#!/bin/bash

# translate original string
echo .
echo "Translate..."
echo .
#python3 translate_batch.py

# save translated string to db
echo .
echo "Export to db..."
echo .
python3 create_db.py

# extract translated string to json
# в случае появления новых приложений и появления новых фраз
# чтобы не переводить уже переведенные фразы
echo .
echo "Export to json..."
echo .
#python3 apply_db_translations.py

# create source from json files
# for dinamic app
echo .
echo "Generate overlays for dynamic app..."
echo .
python3 generate_overlays.py
# for static app
echo .
echo "Generate overlays for static app..."
echo .
python3 generate_overlays_static.py

# make app
echo .
echo "Make dynamic app..."
echo .
python3 create_rro_min.py
echo .
echo "Make static app..."
echo .
python3 create_rro_static.py


