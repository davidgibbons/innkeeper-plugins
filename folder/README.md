# folder

A catalog over a folder of character cards and lorebooks: PNG, JSON, and
CharX cards, and lorebook JSON. Innkeeper searches it and adopts single
items into the library without importing the whole folder.

## Setup

Set **Folder** to the folder's path as the core sees it, such as `/archive`,
then start an ingest from the Archive page. Run another ingest after the
folder changes.

## Behavior

- An item's ID is its path inside the folder.
- A changed file adds a version. A deleted file marks its item removed:
  search stops listing it, but cards adopted from it keep their source.
- Only the latest version of an unchanged file has an avatar. After a file
  changes, ingest again to adopt it with its image.
- Hidden folders, and files that aren't cards or lorebooks, are skipped.
