cd /home/user/projects/deepal-HU-translate

# перед стартом — автопроверка и self-test:
python3 validate.py
python3 test_improve_selftest.py

# точечно (безопасно: только дефектные строки):
API_URL=http://10.0.0.128:11434/v1/chat/completions API_KEY=ollama \
API_MODEL=qwen3.8:27b API_DEBUG=1 \
python3 improve_translations.py --defective

# полностью (~17 800 строк, часы: фоновый, с resume):
API_URL=http://10.0.0.128:11434/v1/chat/completions API_KEY=ollama \
API_MODEL=qwen3.8:27b API_BATCH_SIZE=8 API_TIMEOUT=600 API_DEBUG=1 \
nohup python3 improve_translations.py --all --resume > /dev/null 2>&1 &

# посмотреть прогресс:
python3 -c "import json;print(len(json.load(open('translations/improve_progress.json'))['done']))"
tail -f logs/improve_translations.log

# после завершения:
python3 validate.py
python3 generate_overlays.py && python3 create_rro_min.py
python3 generate_overlays_static.py && python3 create_rro_static.py
python3 validate.py
