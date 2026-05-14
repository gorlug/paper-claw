# Plan

This repo is for building a cli document management tool.

## Goal

PDFs come into an inbox, are transcribed and given a sensible file name.

## Structure

There is an inbox directory where PDFs go.
Whenever the cli is triggered, all files in the inbox are processed:

1. Transcription of the contents.
2. Renaming the file with a sensible name.
3. Moving the file to the library output directory.

Suggested Directory Structure (Sidecar Pattern):
Instead of just dumping renamed PDFs into /library use a "Sidecar" pattern:

- /library/2026-05-13_Finanzamt_Letter/
    - document.pdf (The original file)
    - transcript.md (The OCR/LLM output)
    - metadata.json (Machine-readable tags)

## Infrastructure

Go is being used as the programming language for this project.
Secrets are injected via Infisical.
