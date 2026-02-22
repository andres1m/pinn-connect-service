import os
import json
import sys

def main():
    data_dir = os.environ.get('DATA_DIR', '.')
    input_dir = os.environ.get('INPUT_DIR', '.')
    output_dir = os.environ.get('RESULT_DIR', '.')
    
    data_path = os.path.join(data_dir,'data.json')
    input_path = os.path.join(input_dir, 'input.json')
    
    print(f"Starting mock process...")
    print(f"Output directory: {output_dir}")
    
    try:
        if not os.path.exists(data_path):
            raise FileNotFoundError(f"Data file not found: {data_path}")
            
        with open(data_path, 'r') as f:
            model_data = json.load(f)
            
        if not os.path.exists(input_path):
            raise FileNotFoundError(f"Input file not found: {input_path}")
            
        with open(input_path, 'r') as f:
            input_values = json.load(f)
            
        x = input_values.get('x', 0)
        weight = model_data.get('weight', 1.0)
        bias = model_data.get('bias', 0.0)
        
        result_value = x * weight + bias

        result = {
            "status": "success",
            "input": {"x": x},
            "model_params": {"weight": weight, "bias": bias},
            "output": result_value
        }
        
        os.makedirs(output_dir, exist_ok=True)
        result_path = os.path.join(output_dir, 'result.json')
        
        with open(result_path, 'w') as f:
            json.dump(result, f, indent=4)
            
        print(f"Success! Result saved to {result_path}")
        
    except Exception as e:
        print(f"Error during execution: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
