import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--name", help="Who to greet.")
print(parser.parse_args())
