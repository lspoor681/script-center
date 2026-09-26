"""The Python side of the parameter harvester.

Python is read with the interpreter's own ``ast`` module rather than with a
hand-written parser or a set of regular expressions. ``argparse`` describes its
arguments in ordinary function calls, so a text-level reader has to guess at
which calls are argument definitions, and every guess that is wrong produces a
form that silently omits an input. The tree settles it: ``add_argument`` is a
call, and the keyword arguments are literals that can be read exactly.

Nothing here imports the script or executes any of it. ``argparse`` builds its
parser at run time, so the only way to see the finished object would be to run
the user's code, and running untrusted code in order to describe it is not an
acceptable trade for a form. Everything below is a static read of the tree.

One record is written per requested path, in the order requested, on one line
each. A path that cannot be read still produces a record carrying the reason, so
that a single unreadable file does not hide every other script in the batch.
"""

import ast
import json
import sys

# A parser is the object an ArgumentParser or a subparser is built on. The
# harvester looks for calls made on one of these names, which covers the common
# shapes: a bare ArgumentParser(), a parser passed into a function as `parser`,
# and a subclass of ArgumentParser.
PARSER_NAMES = {"parser", "arg_parser", "argparser", "ap", "cli"}

# Actions that make an argument a flag rather than a value.
FLAG_ACTIONS = {"store_true", "store_false", "count", "append_const", "store_const"}

# Actions that collect more than one value.
REPEATING_ACTIONS = {"append", "extend"}

# Types that mean a filesystem path rather than free text. A path gets a browse
# button in the form, which is only correct if the value really is a path.
PATH_TYPES = {"Path", "PurePath", "PosixPath", "PurePosixPath", "WindowsPath",
              "PureWindowsPath", "FilePath", "DirectoryPath"}

# Python types mapped to the editor kind the UI should offer.
SCALAR_KINDS = {
    "int": "int",
    "float": "float",
    "complex": "float",
    "bool": "bool",
    "str": "string",
    "bytes": "string",
    "Decimal": "float",
    "Fraction": "float",
}


def literal(node):
    """Evaluate a node that is expected to be a literal, or return None.

    A keyword argument is not guaranteed to be a constant. `type=some_factory`
    or `default=build_default()` are perfectly ordinary argparse code, and
    reading them as a value would report whatever the factory returns rather
    than what the author wrote. None means "not knowable statically", which the
    caller turns into a warning rather than a wrong form control.
    """
    if node is None:
        return None
    try:
        return ast.literal_eval(node)
    except (ValueError, SyntaxError, TypeError, MemoryError, RecursionError):
        return None


def callee_name(call):
    """Return the method name a call is made on, or None.

    `parser.add_argument(...)` yields "add_argument". A call on a subscript or a
    comprehension has no method name, which is how dynamically built parsers are
    recognised and reported.
    """
    func = call.func
    if isinstance(func, ast.Attribute):
        return func.attr
    if isinstance(func, ast.Name):
        return func.id
    return None


def called_on_parser(call):
    """Whether a call is made on something that looks like a parser."""
    func = call.func
    if not isinstance(func, ast.Attribute):
        return False
    receiver = func.value
    if isinstance(receiver, ast.Name):
        return receiver.id in PARSER_NAMES
    if isinstance(receiver, ast.Call):
        return callee_name(receiver) in ("ArgumentParser", "Parser")
    if isinstance(receiver, ast.Attribute):
        # `self.parser.add_argument(...)`
        return receiver.attr in PARSER_NAMES
    return False


def type_name(node):
    """The name of a `type=` argument, as written.

    `type=int` is a Name and `type=pathlib.Path` is an Attribute, so the dotted
    spelling is rebuilt from the parts rather than taken from one field.
    """
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        base = type_name(node.value)
        return "%s.%s" % (base, node.attr) if base else node.attr
    if isinstance(node, ast.Call):
        return callee_name(node)
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return node.value
    return ""


def kind_for(type_text, action):
    """Choose the editor kind for an argument from its type and action."""
    if action in FLAG_ACTIONS:
        return "bool"
    if action in REPEATING_ACTIONS:
        return "array"
    short = type_text.rsplit(".", 1)[-1]
    if short in PATH_TYPES:
        return "path"
    if short in SCALAR_KINDS:
        return SCALAR_KINDS[short]
    if not type_text:
        return "string"
    # A custom type function is a string in the author's intent but the value is
    # not necessarily text, so it is left as other rather than guessed at.
    return "other"


def value_record(node):
    """Describe a default value the way the report model expects.

    Two things are reported for a default. The text the author wrote, which is
    what a reader comparing the form against the source needs to see, and a
    plainer rendering of the value, which is what a form control can be
    pre-filled with. A default of ``"fast"`` has a source of ``'fast'`` with its
    quotes and a display of ``fast`` without them.

    A default that is computed at run time is marked as an expression instead.
    That is a different thing from having no default at all, and a form that
    silently showed an empty box would suggest the script wants an empty value.
    """
    value = literal(node)
    if value is None:
        return {"source": "", "isExpression": True}
    if isinstance(value, (list, tuple)):
        display = ", ".join(str(item) for item in value)
    else:
        display = str(value)
    unparse = getattr(ast, "unparse", None)
    if unparse is not None:
        try:
            source = unparse(node)
        except Exception:
            source = repr(value)
    else:
        # ast.unparse arrived in 3.9. Repr is a worse rendering but it is never
        # wrong about the value itself.
        source = repr(value)
    return {"source": source, "display": display}


def parse_add_argument(call):
    """Turn one add_argument call into a parameter record, or None.

    argparse takes each option string as its own positional argument, so
    `add_argument("--server", "-s")` has two of them. Every one is read, because
    dropping the short form would lose the alias the user most likely types.
    """
    if not call.args:
        return None

    flags = []
    for node in call.args:
        # `add_argument(*FLAGS, **kw)` builds its options at run time.
        if isinstance(node, ast.Starred):
            return None
        if not isinstance(node, ast.Constant):
            return None
        if not isinstance(node.value, str):
            return None
        flags.append(node.value)
    if not flags:
        return None

    keywords = {}
    for keyword in call.keywords:
        # `add_argument(**opts)` passes the whole definition at run time.
        if keyword.arg is None:
            return None
        if keyword.arg in keywords:
            return None
        keywords[keyword.arg] = keyword.value

    action_node = keywords.get("action")
    action = literal(action_node) if action_node is not None else None
    if action is not None and not isinstance(action, str):
        action = None

    type_node = keywords.get("type")
    declared_type = type_name(type_node) if type_node is not None else ""

    choices = literal(keywords.get("choices")) if "choices" in keywords else None
    if choices is not None and not isinstance(choices, (list, tuple)):
        choices = None

    help_node = keywords.get("help")
    help_text = literal(help_node) if help_node is not None else None
    if help_text is not None and not isinstance(help_text, str):
        help_text = None

    default_node = keywords.get("default")
    default = value_record(default_node) if default_node is not None else None

    nargs = literal(keywords.get("nargs")) if "nargs" in keywords else None
    metavar = literal(keywords.get("metavar")) if "metavar" in keywords else None

    positional = not any(flag.startswith("-") for flag in flags)

    if positional:
        # A positional's first argument is its name, and it carries no dash.
        name = flags[0]
        aliases = []
        options = flags
    else:
        options = [flag for flag in flags if flag.startswith("-")]
        # The primary name is the first long option, because it reads better in
        # a form row than a single dash and a letter.
        longs = [flag for flag in options if flag.startswith("--")]
        name = longs[0] if longs else options[0]
        aliases = [flag.lstrip("-") for flag in options
                   if flag.lstrip("-") != name.lstrip("-")]

    record = {
        "name": name.lstrip("-") if not positional else name,
        "aliases": [alias for alias in aliases if alias],
        "kind": kind_for(declared_type, action),
        "typeName": declared_type,
        "required": bool(literal(keywords.get("required")) is True) if "required" in keywords else positional,
        "position": 0,
        "fromPipeline": False,
    }

    if positional:
        # Positional arguments are supplied in the order they are declared.
        record["position"] = -1  # replaced by the caller with the running count

    if default is not None:
        record["default"] = default
    if help_text:
        record["help"] = help_text
    if isinstance(metavar, str) and metavar:
        record["metavar"] = metavar
    if isinstance(nargs, str) and nargs in ("+", "*", "..."):
        record["kind"] = "array"

    constraints = []
    if choices:
        values = [item for item in choices if isinstance(item, (str, int, float, bool))]
        if values:
            constraints.append({
                "kind": "set",
                "values": [str(item) for item in values],
                "label": "one of: %s" % ", ".join(str(item) for item in values),
            })
            # A closed set is worth more than the declared type: the value can
            # only be one of a few things, so the form should offer them rather
            # than a text box. A flag or a repeatable option stays as it is,
            # because neither is a single choice.
            if record["kind"] in ("string", "other"):
                record["kind"] = "enum"
    if constraints:
        record["constraints"] = constraints

    if isinstance(default, dict) and default.get("isExpression"):
        record.setdefault("warnings", []).append(
            "the default value for %s is computed at run time and cannot be pre-filled" % name)
    if type_node is not None and not declared_type:
        record.setdefault("warnings", []).append(
            "the type of %s is computed at run time" % name)

    return record


def first_line_summary(docstring):
    """The one-line summary a docstring opens with, or empty."""
    if not docstring:
        return ""
    lines = [line.strip() for line in docstring.strip().splitlines()]
    for line in lines:
        if line:
            return line
    return ""


def remainder_of_docstring(docstring, summary):
    """Everything in a docstring after its summary line."""
    if not docstring:
        return ""
    lines = docstring.strip().splitlines()
    index = 0
    while index < len(lines) and not lines[index].strip():
        index += 1
    if index < len(lines) and lines[index].strip() == summary:
        index += 1
    return "\n".join(lines[index:]).strip()


def find_parser_calls(tree):
    """Collect every add_argument call in the file, in source order.

    Source order is what makes the declaration order of positional arguments
    meaningful, and the call line is a stable sort key across files.
    """
    calls = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        if callee_name(node) != "add_argument":
            continue
        if not called_on_parser(node):
            continue
        calls.append(node)
    calls.sort(key=lambda call: (call.lineno, call.col_offset))
    return calls


def find_parser_options(tree):
    """Read the description and epilog from the ArgumentParser constructor."""
    found = {}
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        if callee_name(node) != "ArgumentParser":
            continue
        for keyword in node.keywords:
            if keyword.arg in ("description", "epilog"):
                value = literal(keyword.value)
                if isinstance(value, str):
                    found.setdefault(keyword.arg, value)
        break
    return found


def find_python_requirement(tree):
    """The minimum Python version the script checks for, as written.

    The idiom is a comparison against sys.version_info, usually inside a guard
    that exits. Reading the tuple is exact, so the requirement is only reported
    when the shape is the familiar one.
    """
    best = None
    for node in ast.walk(tree):
        if not isinstance(node, ast.Compare):
            continue
        left = node.left
        if not (isinstance(left, ast.Attribute) and left.attr == "version_info"):
            continue
        if not isinstance(left.value, ast.Name) or left.value.id != "sys":
            continue
        for op, comparator in zip(node.ops, node.comparators):
            if not isinstance(op, ast.Lt):
                continue
            if isinstance(comparator, ast.Tuple) and comparator.elts:
                numbers = [literal(element) for element in comparator.elts]
                if all(isinstance(number, int) for number in numbers) and numbers:
                    version = ".".join(str(number) for number in numbers[:2])
                    if best is None or version > best:
                        best = version
    return best or ""


def harvest(path):
    """Build one report for one script."""
    record = {
        "path": path,
        "params": [],
        "help": {
            "source": "none",
            "synopsis": "",
            "description": "",
            "notes": "",
            "links": [],
            "examples": [],
            "params": {},
        },
        "requirements": {
            "modules": [],
            "runAsAdministrator": False,
            "psVersion": "",
            "psEditions": [],
        },
        "warnings": [],
    }

    try:
        with open(path, "rb") as handle:
            source = handle.read()
    except OSError as error:
        record["warnings"].append("cannot read the file: %s" % error)
        return record

    try:
        tree = ast.parse(source, filename=path)
    except SyntaxError as error:
        # A script that does not parse is still worth listing. The reason is
        # reported so the user can open it and fix it.
        record["warnings"].append(
            "the file does not parse as Python: %s" % (error.msg,))
        return record

    # The module docstring is the author's own description, so it is preferred
    # over the parser's description when both are present.
    docstring = ast.get_docstring(tree) or ""

    options = find_parser_options(tree)
    synopsis = first_line_summary(docstring) or first_line_summary(options.get("description", ""))
    description = remainder_of_docstring(docstring, first_line_summary(docstring))
    if not description and options.get("description"):
        description = options["description"]

    if synopsis:
        record["help"]["synopsis"] = synopsis
        record["help"]["source"] = "docComment"
    if description:
        record["help"]["description"] = description
        record["help"]["source"] = "docComment"
    if options.get("epilog"):
        record["help"]["notes"] = options["epilog"]

    positional_index = 0
    for call in find_parser_calls(tree):
        param = parse_add_argument(call)
        if param is None:
            record["warnings"].append(
                "an argument defined on line %d is built at run time and is not shown"
                % call.lineno)
            continue
        warnings = param.pop("warnings", [])
        if param["position"] == -1:
            positional_index += 1
            param["position"] = positional_index
        param.pop("metavar", None)
        if warnings:
            record["warnings"].extend(warnings)
        record["params"].append(param)

    # Subcommands turn a flat argument list into a tree, which the report model
    # does not describe. Saying so is better than showing a form that would send
    # the wrong arguments.
    for node in ast.walk(tree):
        if isinstance(node, ast.Call) and callee_name(node) == "add_subparsers":
            record["warnings"].append(
                "this script has subcommands, whose arguments are not shown; run it "
                "with --help to see them")
            break

    if not record["params"]:
        for node in ast.walk(tree):
            if isinstance(node, ast.Call) and callee_name(node) == "add_argument":
                record["warnings"].append(
                    "arguments are added to something other than a parser, so they "
                    "could not be identified")
                break

    version = find_python_requirement(tree)
    if version:
        record["requirements"]["pythonVersion"] = version

    return record


def main():
    paths = sys.argv[1:]
    for path in paths:
        try:
            record = harvest(path)
        except Exception as error:  # a bug here must not lose the other scripts
            record = {
                "path": path,
                "params": [],
                "help": {"source": "none"},
                "requirements": {},
                "warnings": ["could not be inspected: %s: %s"
                             % (type(error).__name__, error)],
            }
        sys.stdout.write(json.dumps(record) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
