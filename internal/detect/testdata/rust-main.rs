use std::env;

fn main() {
    let mut args = env::args();
    println!("{}", args.next().unwrap_or("none"));
}

#[cfg(test)]
mod tests {
    #[test]
    fn it_works() {}
}
