
import logging
# Set up logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

def start_policy(name: str, cfg: Policy, max_workers: int):
    """
    Start the policy for the given configuration.

    Args:
    ----
        name: Policy name
        cfg: Configuration data for the policy.
        max_workers: Maximum number of threads in the pool.

    """
    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = [executor.submit(run_driver, info, cfg.config) for info in cfg.data]

        for future in as_completed(futures):
            try:
                future.result()
            except Exception as e:
                logger.error(f"Error while processing policy {name}: {e}")


def start_agent(cfg: Diode, workers: int):
    """
    Start the diode client and execute policies.

    Args:
    ----
        cfg: Configuration data containing policies.
        workers: Number of workers to be used in the thread pool.

    """

    for policy_name in cfg.policies:
        start_policy(policy_name, cfg.policies.get(policy_name), workers)
        
        
def parse_policy(config_data: str):
    """
    Parse the YAML configuration data into a Config object.
    
    Args:
    ----
        config_data (str): The YAML configuration data as a string.
        
    Returns:
    -------
        Config: The configuration object.
        
    """
    try:
        config = yaml.safe_load(config_data)
        config = resolve_env_vars(config)
        return Config(**config)
    except ValidationError as e:
        raise ParseException(f"Error parsing configuration: {e}")
    except Exception as e:
        raise ParseException(f"Error parsing configuration: {e}")
    u