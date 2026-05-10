FROM python:3.11-slim

# Dipendenze
RUN pip install --no-cache-dir \
    huggingface_hub \
    boto3

COPY downloader.py /usr/local/bin/downloader.py
RUN chmod +x /usr/local/bin/downloader.py

ENTRYPOINT ["python3", "/usr/local/bin/downloader.py"]